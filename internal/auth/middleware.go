package auth

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// SessionContextKey is where the middleware stores the caller's session.
const SessionContextKey = "s3panel_session"

// RequireSession rejects API requests that carry no valid session cookie.
//
// It is the AuthModeIAM replacement for the static bearer-token middleware: that
// one proves a caller is the panel, this one proves which person is asking.
func (a *Authenticator) RequireSession() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			session, ok := a.Load(c)
			if !ok {
				// 401 rather than a redirect: these are XHR calls, and a 302 to the
				// identity provider would fail CORS and surface as an opaque network
				// error instead of "log in again".
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			c.Set(SessionContextKey, session)
			return next(c)
		}
	}
}

// SessionFrom returns the session the middleware stored.
func SessionFrom(c echo.Context) (Session, bool) {
	session, ok := c.Get(SessionContextKey).(Session)
	return session, ok
}
