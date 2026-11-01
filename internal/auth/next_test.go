package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestSafeReturnPath(t *testing.T) {
	at := &Authenticator{BasePath: "/storage"}
	root := &Authenticator{}

	for next, want := range map[string]string{
		"/storage":  "/storage",
		"/storage/": "/storage/",
		"/storage/object-storage/s3-bucket/buckets/ride--receipts?region=teh-1": "/storage/object-storage/s3-bucket/buckets/ride--receipts?region=teh-1",
		"/storage/a%2Fb":                  "/storage/a%2Fb",
		"/storage/x#frag":                 "/storage/x",
		"/storage/x?path=a//b":            "/storage/x?path=a//b",
		"":                                "",
		"storage/x":                       "",
		"https://evil.example.com/":       "",
		"//evil.example.com/storage/":     "",
		"/\\evil.example.com":             "",
		"/storage\\..\\iam":               "",
		"/iam/":                           "",
		"/":                               "",
		"/storagex/":                      "",
		"/storage/../iam/":                "",
		"/storage/%2e%2e/iam/":            "",
		"/storage/./x":                    "",
		"/storage//evil.example.com":      "",
		"/storage/%2F%2Fevil.example.com": "",
		"/storage/x\n":                    "",
		"/storage/x\tSet-Cookie:":         "",
		"/storage/auth/login":             "",
		"/storage/auth/logout":            "",
		"/storage/auth":                   "",
		"javascript:alert(1)":             "",
		"/storage/" + strings.Repeat("a", maxReturnPath): "",
	} {
		if got := at.SafeReturnPath(next); got != want {
			t.Errorf("base /storage: SafeReturnPath(%q) = %q, want %q", next, got, want)
		}
	}

	for next, want := range map[string]string{
		"/":                     "/",
		"/object-storage/x?y=1": "/object-storage/x?y=1",
		"//evil.example.com":    "",
		"/auth/callback":        "",
		"/../x":                 "",
	} {
		if got := root.SafeReturnPath(next); got != want {
			t.Errorf("no base: SafeReturnPath(%q) = %q, want %q", next, got, want)
		}
	}
}

// TestSignInReturnsWhereThePersonWasGoing: a link into the panel from a fresh
// session must land on what it linked to, not on the bucket list.
func TestSignInReturnsWhereThePersonWasGoing(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	want := "/storage/object-storage/s3-bucket/buckets/ride?region=teh-1"
	rec := signIn(t, e, idp, "/storage", "?next="+url.QueryEscape(want))
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("landed on %q, want %q", got, want)
	}

	// An unsafe next is dropped at login, and the sign-in lands as usual.
	rec = signIn(t, e, idp, "/storage", "?next="+url.QueryEscape("https://evil.example.com/"))
	if got := rec.Header().Get("Location"); got != "/storage/" {
		t.Errorf("unsafe next: landed on %q, want /storage/", got)
	}
}

// TestCallbackRechecksATamperedReturnPath: the state cookie is the browser's
// to edit, so the callback validates the return path again.
func TestCallbackRechecksATamperedReturnPath(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/storage/auth/login", nil))
	authorize, _ := url.Parse(login.Header().Get("Location"))
	idp.mu.Lock()
	idp.nonce = authorize.Query().Get("nonce")
	idp.mu.Unlock()

	var state *http.Cookie
	for _, c := range cookiesNamed(login, stateCookieName) {
		state = c
	}
	state.Value += "." + base64.RawURLEncoding.EncodeToString([]byte("//evil.example.com/"))

	req := httptest.NewRequest(http.MethodGet, "/storage/auth/callback?code=c&state="+url.QueryEscape(authorize.Query().Get("state")), nil)
	req.AddCookie(state)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/storage/" {
		t.Errorf("tampered next: %d to %q, want 302 to /storage/", rec.Code, rec.Header().Get("Location"))
	}
}
