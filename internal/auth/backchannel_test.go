package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

func logoutToken(idp *fakeIdP, edit func(map[string]any)) map[string]any {
	claims := map[string]any{
		"iss":    idp.URL,
		"aud":    "panel",
		"iat":    time.Now().Unix(),
		"exp":    time.Now().Add(2 * time.Minute).Unix(),
		"jti":    "j-1",
		"sub":    "u-alice",
		"sid":    "sid-123",
		"events": map[string]any{backchannelEvent: map[string]any{}},
	}
	if edit != nil {
		edit(claims)
	}
	return claims
}

func postLogoutToken(e *echo.Echo, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"logout_token": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range cookiesNamed(rec, config.DefaultCookieName) {
		if c.Value != "" {
			return c
		}
	}
	t.Fatalf("no session cookie in:\n%s", setCookieHeaders(rec))
	return nil
}

// TestBackchannelLogoutEndsTheSession: once the provider says sid-123 ended,
// a cookie carrying it is refused, though it is sealed and unexpired. Other
// sessions are untouched.
func TestBackchannelLogoutEndsTheSession(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "/storage")
	e := echo.New()
	a.RegisterRoutes(e.Group("/storage"))

	ended := sessionCookieOf(t, signIn(t, e, idp, "/storage", ""))
	idp.mu.Lock()
	idp.sid = "sid-other"
	idp.mu.Unlock()
	other := sessionCookieOf(t, signIn(t, e, idp, "/storage", ""))

	if rec := me(e, "/storage/auth/me", ended); rec.Code != http.StatusOK {
		t.Fatalf("before: /auth/me = %d", rec.Code)
	}
	rec := postLogoutToken(e, "/storage/auth/backchannel-logout", sign(t, idp.key, logoutToken(idp, nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("back-channel logout = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if rec := me(e, "/storage/auth/me", ended); rec.Code != http.StatusUnauthorized {
		t.Errorf("after: the ended session's /auth/me = %d, want 401", rec.Code)
	}
	if rec := me(e, "/storage/auth/me", other); rec.Code != http.StatusOK {
		t.Errorf("after: another session's /auth/me = %d, want 200", rec.Code)
	}
}

// TestBackchannelLogoutRefusesBadTokens: only a fresh logout token the
// provider signed for this client may end a session.
func TestBackchannelLogoutRefusesBadTokens(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	e := echo.New()
	a.RegisterRoutes(e)
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{
		"empty":         "",
		"garbage":       "not-a-jwt",
		"other key":     sign(t, stranger, logoutToken(idp, nil)),
		"other client":  sign(t, idp.key, logoutToken(idp, func(c map[string]any) { c["aud"] = "cost" })),
		"other issuer":  sign(t, idp.key, logoutToken(idp, func(c map[string]any) { c["iss"] = "https://evil" })),
		"no event":      sign(t, idp.key, logoutToken(idp, func(c map[string]any) { delete(c, "events") })),
		"an ID token":   sign(t, idp.key, logoutToken(idp, func(c map[string]any) { c["nonce"] = "n" })),
		"stale":         sign(t, idp.key, logoutToken(idp, func(c map[string]any) { c["iat"] = time.Now().Add(-time.Hour).Unix(); delete(c, "exp") })),
		"expired":       sign(t, idp.key, logoutToken(idp, func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() })),
		"no iat":        sign(t, idp.key, logoutToken(idp, func(c map[string]any) { delete(c, "iat") })),
		"no sid":        sign(t, idp.key, logoutToken(idp, func(c map[string]any) { delete(c, "sid") })),
		"future issued": sign(t, idp.key, logoutToken(idp, func(c map[string]any) { c["iat"] = time.Now().Add(time.Hour).Unix() })),
	} {
		if rec := postLogoutToken(e, "/auth/backchannel-logout", token); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, rec.Code)
		}
	}
	if a.signedOut.has("sid-123") {
		t.Error("a refused token still ended the session")
	}
}

func TestSIDDenyListExpires(t *testing.T) {
	l := newSIDDenyList()
	l.add("a", time.Now().Add(time.Hour))
	l.add("b", time.Now().Add(-time.Second))
	if !l.has("a") || l.has("b") || l.has("") {
		t.Errorf("has: a=%v b=%v", l.has("a"), l.has("b"))
	}
	var nilList *sidDenyList
	if nilList.has("a") {
		t.Error("a nil list matched")
	}
}

// TestLogoutRevokesTheRefreshToken: sign-out revokes the refresh token at the
// provider, authenticating as a confidential client with client_secret_basic.
func TestLogoutRevokesTheRefreshToken(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	a.cfg.ClientSecret = "s3cr3t"
	a.RevokeRefreshToken = true
	e := echo.New()
	a.RegisterRoutes(e)

	session := sessionCookieOf(t, signIn(t, e, idp, "", ""))
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}

	idp.mu.Lock()
	defer idp.mu.Unlock()
	if len(idp.revoked) != 1 {
		t.Fatalf("%d revocations, want 1", len(idp.revoked))
	}
	got := idp.revoked[0]
	if got.form.Get("token") != "rt-1" || got.form.Get("token_type_hint") != "refresh_token" {
		t.Errorf("revocation form = %v", got.form)
	}
	if !strings.HasPrefix(got.authorization, "Basic ") || got.form.Get("client_id") != "" {
		t.Errorf("client authentication: Authorization %q, client_id %q", got.authorization, got.form.Get("client_id"))
	}
}

// TestLogoutSurvivesAFailedRevocation and TestLogoutDoesNotRevokeByDefault.
func TestLogoutRevocationIsBestEffortAndOptIn(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	e := echo.New()
	a.RegisterRoutes(e)
	logout := func() int {
		session := sessionCookieOf(t, signIn(t, e, idp, "", ""))
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(session)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := logout(); code != http.StatusNoContent {
		t.Fatalf("logout = %d", code)
	}
	idp.mu.Lock()
	if len(idp.revoked) != 0 {
		t.Errorf("revoked with RevokeRefreshToken off")
	}
	idp.revokeStatus = http.StatusServiceUnavailable
	idp.mu.Unlock()

	a.RevokeRefreshToken = true
	if code := logout(); code != http.StatusNoContent {
		t.Errorf("logout with the provider failing = %d, want 204", code)
	}
	idp.mu.Lock()
	defer idp.mu.Unlock()
	if len(idp.revoked) != 1 || idp.revoked[0].form.Get("client_id") != "panel" {
		t.Errorf("public client revocation = %+v", idp.revoked)
	}
}
