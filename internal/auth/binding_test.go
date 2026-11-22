package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// boundSignIn signs in through a bound authenticator and returns the cookies
// the browser would then hold.
func boundSignIn(t *testing.T, e *echo.Echo, idp *fakeIdP, base string) (session, companion *http.Cookie) {
	t.Helper()
	rec := signIn(t, e, idp, base, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d: %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		switch {
		case c.Name == config.DefaultCookieName && c.Value != "":
			session = c
		case c.Name == bindCookieSecure && c.Value != "":
			companion = c
		}
	}
	if session == nil || companion == nil {
		t.Fatalf("sign-in set no session or no companion:\n%s", setCookieHeaders(rec))
	}
	return session, companion
}

func me(e *echo.Echo, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestBoundSessionNeedsItsOwnCompanion: a session cookie is accepted only
// with the host-only companion it was issued with. Another person's sealed
// session, tossed at the panel from a sibling host, arrives without it.
func TestBoundSessionNeedsItsOwnCompanion(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	a.BindSession = true
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	victim, victimBind := boundSignIn(t, e, idp, "/storage")
	attacker, _ := boundSignIn(t, e, idp, "/storage")

	// __Host- terms: Secure, Path=/, no Domain.
	if !victimBind.Secure || victimBind.Path != "/" || victimBind.Domain != "" || !victimBind.HttpOnly {
		t.Errorf("companion = %+v, want Secure, HttpOnly, Path=/, host-only", victimBind)
	}

	if rec := me(e, "/storage/auth/me", victim, victimBind); rec.Code != http.StatusOK {
		t.Fatalf("own session with its companion = %d, want 200", rec.Code)
	}
	if rec := me(e, "/storage/auth/me", victim); rec.Code != http.StatusUnauthorized {
		t.Errorf("session without a companion = %d, want 401", rec.Code)
	}
	if rec := me(e, "/storage/auth/me", attacker, victimBind); rec.Code != http.StatusUnauthorized {
		t.Errorf("tossed session with the victim's companion = %d, want 401", rec.Code)
	}
}

// TestBindingRefusesUnboundSessions: a session issued before binding was on
// carries no binding, and is refused once it is on.
func TestBindingRefusesUnboundSessions(t *testing.T) {
	a := newLogoutAuthenticator(t)
	a.cfg.CookieSecure = true
	session := Session{Subject: "u-alice", ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour)}
	value, err := a.codec.Encode(session)
	if err != nil {
		t.Fatal(err)
	}
	load := func() bool {
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.AddCookie(&http.Cookie{Name: config.DefaultCookieName, Value: value})
		_, ok := a.Load(echo.New().NewContext(req, httptest.NewRecorder()))
		return ok
	}
	if !load() {
		t.Fatal("unbound authenticator refused an unbound session")
	}
	a.BindSession = true
	if load() {
		t.Error("bound authenticator accepted an unbound session")
	}
}

// TestUnboundSignInSetsNoCompanion: the standalone default stays as it was.
func TestUnboundSignInSetsNoCompanion(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	e := echo.New()
	a.RegisterRoutes(e)

	rec := signIn(t, e, idp, "", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d", rec.Code)
	}
	for _, name := range []string{bindCookieSecure, bindCookieInsecure, stateCookieBound} {
		if got := cookiesNamed(rec, name); len(got) != 0 {
			t.Errorf("unbound sign-in set %s", name)
		}
	}
	if s := sessionFromResponse(t, a, rec); s.Binding != "" {
		t.Errorf("unbound session carries a binding")
	}
}

// TestBindingOnPlainHTTP: with cookie_secure off (http://*.localhost) the
// companion goes out unprefixed and not Secure, which a browser accepts there.
func TestBindingOnPlainHTTP(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	a.cfg.CookieSecure = false
	a.BindSession = true
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/storage/auth/login", nil))
	if got := cookiesNamed(login, stateCookieName); len(got) != 1 || got[0].Path != "/storage" {
		t.Errorf("plain-HTTP state cookie = %v, want the path-scoped one", got)
	}

	rec := signIn(t, e, idp, "/storage", "")
	binds := cookiesNamed(rec, bindCookieInsecure)
	if len(binds) != 1 || binds[0].Secure || binds[0].Path != "/" || binds[0].Value == "" {
		t.Fatalf("companion = %v, want one unprefixed, not Secure, Path=/", binds)
	}
	var session *http.Cookie
	for _, c := range cookiesNamed(rec, config.DefaultCookieName) {
		if c.Value != "" {
			session = c
		}
	}
	if got := me(e, "/storage/auth/me", session, binds[0]); got.Code != http.StatusOK {
		t.Errorf("/auth/me = %d, want 200", got.Code)
	}
}

// TestBoundSignInStateIsHostOnly: the sign-in state cookie is __Host- when
// bound, so a sibling host cannot plant its own state and finish a sign-in in
// the person's browser with the attacker's authorization code.
func TestBoundSignInStateIsHostOnly(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	a.BindSession = true
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/storage/auth/login", nil))
	got := cookiesNamed(login, stateCookieBound)
	if len(got) != 1 || got[0].Path != "/" || !got[0].Secure || got[0].Domain != "" {
		t.Fatalf("state cookie = %v, want one __Host- cookie", got)
	}
	if len(cookiesNamed(login, stateCookieName)) != 0 {
		t.Error("the path-scoped state cookie is still set")
	}
}

// TestTwoStateCookiesAreRefused: one of them is someone else's.
func TestTwoStateCookiesAreRefused(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	e := echo.New()
	a.RegisterRoutes(e)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=s", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "s.n"})
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "s.n"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("callback with two state cookies = %d, want 400", rec.Code)
	}
}

// TestTwoSessionCookiesAreRefused: the panel sets one session cookie; a second
// was tossed from elsewhere. The request is refused and every copy the
// browser could have sent is cleared, at each parent domain and path.
func TestTwoSessionCookiesAreRefused(t *testing.T) {
	a := newLogoutAuthenticator(t)
	a.BasePath = "/storage"
	a.cfg.CookieSecure = true
	good := Session{Subject: "u-alice", ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour)}
	tossed := Session{Subject: "u-mallory", ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour)}

	req := httptest.NewRequest(http.MethodGet, "http://panel.apps.example.com/storage/api/object/list", nil)
	for _, s := range []Session{tossed, good} {
		v, err := a.codec.Encode(s)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: config.DefaultCookieName, Value: v})
	}
	rec := httptest.NewRecorder()
	if _, ok := a.Load(echo.New().NewContext(req, rec)); ok {
		t.Fatal("a request with two session cookies was accepted")
	}

	cleared := map[string]bool{}
	for _, c := range cookiesNamed(rec, config.DefaultCookieName) {
		if c.MaxAge < 0 {
			cleared[strings.TrimPrefix(c.Domain, ".")+"|"+c.Path] = true
		}
	}
	for _, want := range []string{
		"|/storage", "|/", "|/storage/api",
		"apps.example.com|/storage/api", "apps.example.com|/storage/api/object/list",
		"example.com|/storage", "panel.apps.example.com|/storage",
	} {
		if !cleared[want] {
			t.Errorf("no clear for %q; cleared %v", want, cleared)
		}
	}
	if cleared["com|/"] {
		t.Error("cleared at the TLD")
	}
}

func TestParentDomains(t *testing.T) {
	for host, want := range map[string]string{
		"panel.apps.example.com:443": "panel.apps.example.com apps.example.com example.com",
		"panel.localhost:8080":       "panel.localhost",
		"localhost":                  "",
		"10.0.0.1:8080":              "",
		"[::1]:8080":                 "",
	} {
		if got := strings.Join(parentDomains(host), " "); got != want {
			t.Errorf("parentDomains(%q) = %q, want %q", host, got, want)
		}
	}
}
