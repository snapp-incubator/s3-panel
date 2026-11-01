package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// newLogoutAuthenticator builds an Authenticator that can encode and decode a
// session cookie, which is what Logout has to read before clearing it.
func newLogoutAuthenticator(t *testing.T) *Authenticator {
	t.Helper()
	codec, err := NewCookieCodec("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	if err != nil {
		t.Fatalf("NewCookieCodec: %v", err)
	}
	return &Authenticator{
		codec:              codec,
		refreshGroupFields: refreshGroupFields{refreshMemo: newRefreshMemo()},
	}
}

// TestLogoutRevokesStorageCredentials.
//
// Signing out has to reach past the cookie. A storage credential this console
// obtained lives for hours, works from anywhere, and is not shown to anyone once
// the browser forgets the session — so clearing the cookie alone leaves a real
// credential live with nothing pointing at it.
func TestLogoutRevokesStorageCredentials(t *testing.T) {
	a := newLogoutAuthenticator(t)
	session := Session{
		Subject: "u-alice", Email: "alice@example.com", AccessToken: "tok",
		ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour),
	}

	var got Session
	called := 0
	a.OnLogout = func(_ context.Context, s Session) { called++; got = s }

	rec := logoutRequest(t, a, session)

	if called != 1 {
		t.Fatalf("OnLogout called %d times, want 1", called)
	}
	if got.AccessToken != "tok" {
		t.Errorf("the hook got no access token; it cannot authenticate the revoke")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	assertCookieCleared(t, rec)
}

// TestLogoutSucceedsWhenTheRevokeFails. A revoke that cannot be delivered must
// not trap the person in a signed-in state — the credential still expires on its
// own, and refusing to sign out is strictly worse.
func TestLogoutSucceedsWhenTheRevokeFails(t *testing.T) {
	a := newLogoutAuthenticator(t)
	a.OnLogout = func(context.Context, Session) { _ = errors.New("control endpoint is down") }

	rec := logoutRequest(t, a, Session{
		Subject: "u-alice", AccessToken: "tok",
		ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour),
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 even when the revoke fails", rec.Code)
	}
	assertCookieCleared(t, rec)
}

// TestLogoutWithoutASessionDoesNotRevoke: nothing to revoke, and no session to
// hand the hook.
func TestLogoutWithoutASessionDoesNotRevoke(t *testing.T) {
	a := newLogoutAuthenticator(t)
	called := 0
	a.OnLogout = func(context.Context, Session) { called++ }

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := a.Logout(e.NewContext(httptest.NewRequest(http.MethodPost, "/auth/logout", nil), rec)); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if called != 0 {
		t.Fatalf("OnLogout ran %d times with no session", called)
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func logoutRequest(t *testing.T, a *Authenticator, session Session) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := a.codec.Encode(session)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: a.cookieName(), Value: encoded})
	rec := httptest.NewRecorder()
	if err := a.Logout(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	return rec
}

func assertCookieCleared(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			return
		}
	}
	t.Error("the session cookie was not cleared")
}

// TestLogoutIsPOSTOnly: signing out revokes storage credentials, so it must
// not be reachable by a GET that any page could trigger with an image tag.
func TestLogoutIsPOSTOnly(t *testing.T) {
	e := echo.New()
	newLogoutAuthenticator(t).RegisterRoutes(e)
	for _, r := range e.Routes() {
		if r.Path == "/auth/logout" && r.Method != http.MethodPost {
			t.Errorf("/auth/logout is routed for %s", r.Method)
		}
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/logout", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /auth/logout = %d, want 405", rec.Code)
	}
}
