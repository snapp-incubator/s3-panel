package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// TestSignInUnderABasePath: on a host shared with other apps, every cookie the
// sign-in sets is scoped to the panel's own path and to no Domain, so the
// browser sends none of them to the other apps; the landing stays under the
// panel; and the Path=/ cookie a root-mounted panel left behind is expired.
func TestSignInUnderABasePath(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/storage/auth/login", nil))
	for _, c := range cookiesNamed(login, stateCookieName) {
		if c.Path != "/storage" {
			t.Errorf("state cookie Path = %q, want /storage", c.Path)
		}
	}

	rec := signIn(t, e, idp, "/storage", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/storage/" {
		t.Errorf("landed on %q, want /storage/", got)
	}

	var session *http.Cookie
	for _, c := range cookiesNamed(rec, config.DefaultCookieName) {
		if c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("no session cookie set:\n%s", setCookieHeaders(rec))
	}
	if session.Path != "/storage" || session.Domain != "" || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie = %+v, want Path=/storage, no Domain, HttpOnly, Secure, SameSite=Lax", session)
	}
	if !hasClearAt(cookiesNamed(rec, stateCookieName), "/storage") {
		t.Errorf("state cookie not cleared at /storage:\n%s", setCookieHeaders(rec))
	}
	for _, name := range []string{config.DefaultCookieName, stateCookieName} {
		if !hasClearAt(cookiesNamed(rec, name), "/") {
			t.Errorf("legacy Path=/ %s not expired:\n%s", name, setCookieHeaders(rec))
		}
	}
}

// TestSignInAtTheRoot is the standalone default, which must not change: Path=/
// throughout, no extra clears, and the configured landing.
func TestSignInAtTheRoot(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	e := echo.New()
	a.RegisterRoutes(e)

	rec := signIn(t, e, idp, "", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback = %d to %q, want 302 to /", rec.Code, rec.Header().Get("Location"))
	}
	for _, c := range rec.Result().Cookies() {
		if c.Path != "/" {
			t.Errorf("cookie %s Path = %q, want /", c.Name, c.Path)
		}
	}
	if n := len(cookiesNamed(rec, config.DefaultCookieName)); n != 1 {
		t.Errorf("%d Set-Cookie for the session, want exactly 1:\n%s", n, setCookieHeaders(rec))
	}
}

// TestLogoutClearsWithTheSamePath: a clear with any other Path leaves the
// cookie in the browser, and sign-out would silently keep the session.
func TestLogoutClearsWithTheSamePath(t *testing.T) {
	session := Session{
		Subject: "u-alice", AccessToken: "tok",
		ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour),
	}
	for _, tc := range []struct {
		base      string
		wantPaths []string
	}{
		{"", []string{"/"}},
		{"/storage", []string{"/storage", "/"}},
	} {
		a := newLogoutAuthenticator(t)
		a.BasePath = tc.base
		rec := logoutRequest(t, a, session)
		cleared := cookiesNamed(rec, config.DefaultCookieName)
		if len(cleared) != len(tc.wantPaths) {
			t.Errorf("base %q: %d clears, want %d:\n%s", tc.base, len(cleared), len(tc.wantPaths), setCookieHeaders(rec))
		}
		for _, p := range tc.wantPaths {
			if !hasClearAt(cleared, p) {
				t.Errorf("base %q: session not cleared at Path=%s:\n%s", tc.base, p, setCookieHeaders(rec))
			}
		}
		for _, c := range cleared {
			if c.Domain != "" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("base %q: clear %+v lacks the attributes it was set with", tc.base, c)
			}
		}
	}
}

func TestPostLoginRedirectIsPrefixAware(t *testing.T) {
	for _, tc := range []struct{ base, configured, want string }{
		{"", "", "/"},
		{"", "/", "/"},
		{"", "/object-storage", "/object-storage"},
		{"/storage", "", "/storage/"},
		{"/storage", "/", "/storage/"},
		{"/storage", "/object-storage", "/storage/object-storage"},
		{"/storage", "/storage/object-storage", "/storage/object-storage"},
		{"/storage", "/storage", "/storage"},
		// Not root-relative: the operator meant it literally.
		{"/storage", "https://panel.example.com/", "https://panel.example.com/"},
		{"/storage", "//elsewhere.example.com/", "//elsewhere.example.com/"},
	} {
		a := &Authenticator{cfg: config.OIDCConfig{PostLoginRedirect: tc.configured}, BasePath: tc.base}
		if got := a.postLoginRedirect(); got != tc.want {
			t.Errorf("base %q, post_login_redirect %q: got %q, want %q", tc.base, tc.configured, got, tc.want)
		}
	}
}
