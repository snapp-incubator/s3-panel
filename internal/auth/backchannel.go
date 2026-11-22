package auth

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// OpenID Connect Back-Channel Logout 1.0.
//
// When a session ends at the provider (sign-out elsewhere, an admin ending it,
// a password reset), the provider POSTs a signed logout token naming the
// session's sid. The cookie is stateless and outlives that moment, so the sid
// goes on a deny-list for as long as a cookie issued for it could still be
// valid, and Load refuses any session carrying it.
//
// The list lives in this process. A panel run as several replicas behind one
// back-channel URL learns of a logout only on the replica the provider
// reached; the others keep accepting the cookie until its access token next
// fails to refresh.

// backchannelEvent is the events member a logout token must carry.
const backchannelEvent = "http://schemas.openid.net/event/backchannel-logout"

// maxLogoutTokenAge refuses a logout token issued longer ago than this, and
// logoutTokenSkew one issued this far in the future.
const (
	maxLogoutTokenAge = 10 * time.Minute
	logoutTokenSkew   = time.Minute
)

// maxLogoutBody bounds the form a back-channel call may send.
const maxLogoutBody = 64 << 10

// maxDeniedSIDs bounds the deny-list. Only the provider can add to it (the
// token is signed), so this is a safety net rather than a limit.
const maxDeniedSIDs = 100_000

// BackchannelLogout handles the provider's logout token.
func (a *Authenticator) BackchannelLogout(c echo.Context) error {
	h := c.Response().Header()
	h.Set("Cache-Control", "no-store")
	refuse := func(why string) error {
		if a.Logger != nil {
			a.Logger.Info("refused a back-channel logout", zap.String("reason", why))
		}
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid_request", "error_description": why,
		})
	}

	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxLogoutBody)
	raw := c.FormValue("logout_token")
	if raw == "" {
		return refuse("no logout_token")
	}
	if a.logoutVerifier == nil || a.signedOut == nil {
		return refuse("back-channel logout is not set up")
	}
	tok, err := a.logoutVerifier.Verify(c.Request().Context(), raw)
	if err != nil {
		return refuse("the logout token failed verification")
	}
	var claims struct {
		SID    string                     `json:"sid"`
		Events map[string]json.RawMessage `json:"events"`
		Nonce  *string                    `json:"nonce"`
	}
	if err := tok.Claims(&claims); err != nil {
		return refuse("the logout token's claims are unreadable")
	}
	if _, ok := claims.Events[backchannelEvent]; !ok {
		return refuse("not a logout token")
	}
	if claims.Nonce != nil {
		// An ID token replayed as a logout token.
		return refuse("a logout token carries no nonce")
	}
	now := time.Now()
	if !tok.Expiry.IsZero() && now.After(tok.Expiry.Add(logoutTokenSkew)) {
		return refuse("the logout token has expired")
	}
	if tok.IssuedAt.IsZero() || now.Sub(tok.IssuedAt) > maxLogoutTokenAge || tok.IssuedAt.Sub(now) > logoutTokenSkew {
		return refuse("the logout token is stale")
	}
	if claims.SID == "" {
		// Ending every session of a subject would need the session's sign-in
		// time; the provider this serves (Keycloak) always names the sid.
		return refuse("the logout token names no session")
	}

	a.signedOut.add(claims.SID, now.Add(a.sessionTTL()))
	if a.Logger != nil {
		a.Logger.Info("the provider ended a session", zap.String("subject", tok.Subject))
	}
	return c.NoContent(http.StatusOK)
}

// sidDenyList holds provider session IDs that ended, each until no cookie
// issued for it can still be valid.
type sidDenyList struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func newSIDDenyList() *sidDenyList { return &sidDenyList{until: map[string]time.Time{}} }

func (l *sidDenyList) add(sid string, until time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.until) >= maxDeniedSIDs {
		l.pruneLocked(time.Now())
	}
	for k := range l.until {
		if len(l.until) < maxDeniedSIDs {
			break
		}
		delete(l.until, k)
	}
	l.until[sid] = until
}

// has reports whether sid ended. A nil list or an empty sid never matches.
func (l *sidDenyList) has(sid string) bool {
	if l == nil || sid == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.until[sid]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(l.until, sid)
		return false
	}
	return true
}

func (l *sidDenyList) pruneLocked(now time.Time) {
	for k, until := range l.until {
		if now.After(until) {
			delete(l.until, k)
		}
	}
}
