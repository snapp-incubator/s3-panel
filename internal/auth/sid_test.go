package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// sessionFromResponse opens the session cookie a response set.
func sessionFromResponse(t *testing.T, a *Authenticator, rec *httptest.ResponseRecorder) Session {
	t.Helper()
	for _, c := range cookiesNamed(rec, config.DefaultCookieName) {
		if c.Value == "" {
			continue
		}
		s, err := a.codec.Decode(c.Value)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return s
	}
	t.Fatalf("no session cookie in:\n%s", setCookieHeaders(rec))
	return Session{}
}

// TestSignInKeepsSIDAndIDToken: the provider's session ID is what a
// back-channel logout or a deny-list would match this session by, and the ID
// token is the hint a provider logout needs. Both used to be dropped at the
// callback.
func TestSignInKeepsSIDAndIDToken(t *testing.T) {
	idp := newFakeIdP(t)
	a := newIdPAuthenticator(t, idp, "")
	e := echo.New()
	a.RegisterRoutes(e)

	rec := signIn(t, e, idp, "", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d: %s", rec.Code, rec.Body.String())
	}
	s := sessionFromResponse(t, a, rec)
	if s.SID != "sid-123" {
		t.Errorf("SID = %q, want sid-123", s.SID)
	}
	if strings.Count(s.IDToken, ".") != 2 {
		t.Errorf("IDToken = %q, want the raw JWT", s.IDToken)
	}

	// /auth/me still hands the SPA no token and no sid.
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	for _, c := range cookiesNamed(rec, config.DefaultCookieName) {
		req.AddCookie(c)
	}
	me := httptest.NewRecorder()
	e.ServeHTTP(me, req)
	for _, leak := range []string{s.IDToken, "sid-123", "access_token", "id_token"} {
		if strings.Contains(me.Body.String(), leak) {
			t.Errorf("/auth/me exposes %q: %s", leak, me.Body.String())
		}
	}
}

// realisticJWT is shaped like a Keycloak RS256 token: compressible JSON
// segments and a 342-character signature that is not.
func realisticJWT(t *testing.T, claimsBytes int) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"Zc1nV6kq9mWkE2Xb3Lr0"}`))
	claims := map[string]any{"pad": strings.Repeat("realm-management account view-profile ", claimsBytes/40)}
	raw, _ := json.Marshal(claims)
	return header + "." + base64.RawURLEncoding.EncodeToString(raw) + "." + incompressibleToken(t, 342)[:342]
}

// TestRealisticSessionFitsWithTheIDToken: three Keycloak tokens in one cookie
// is the new worst case, and it must still fit.
func TestRealisticSessionFitsWithTheIDToken(t *testing.T) {
	codec, err := NewCookieCodec(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(Session{
		Subject:          "6885c70e-a794-4438-9c44-62ccfe845dea",
		Email:            "alice@example.com",
		Name:             "Alice Ahmadi",
		SID:              "4f1c8d2e-6a3b-4c5d-9e7f-0a1b2c3d4e5f",
		AccessToken:      realisticJWT(t, 1000),
		RefreshToken:     realisticJWT(t, 400),
		IDToken:          realisticJWT(t, 800),
		ExpiresAt:        time.Now().Add(5 * time.Minute),
		SessionExpiresAt: time.Now().Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatalf("a realistic session with an ID token does not fit: %v", err)
	}
	if len(encoded) > maxCookieBytes {
		t.Fatalf("%d bytes", len(encoded))
	}
	t.Logf("realistic session cookie: %d of %d bytes", len(encoded), maxCookieBytes)
}

// TestSaveDropsTheIDTokenBeforeTheSession: when the cookie would overflow,
// losing the logout hint is better than losing the sign-in.
func TestSaveDropsTheIDTokenBeforeTheSession(t *testing.T) {
	a := newLogoutAuthenticator(t)
	session := Session{
		Subject: "u-alice", AccessToken: "tok", RefreshToken: "rt",
		IDToken:          incompressibleToken(t, 5000),
		ExpiresAt:        time.Now().Add(time.Hour),
		SessionExpiresAt: time.Now().Add(time.Hour),
	}
	rec := httptest.NewRecorder()
	if err := a.Save(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := sessionFromResponse(t, a, rec)
	if got.IDToken != "" || got.AccessToken != "tok" || got.RefreshToken != "rt" {
		t.Errorf("saved %+v, want the session without its ID token", got)
	}
}
