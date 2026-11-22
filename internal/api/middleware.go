package api

import (
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
)

// CORSMiddleware answers cross-origin requests from the configured origins.
// With none configured it adds nothing at all: echo would read an empty list
// as "*", and the panel's own pages are same-origin and need no CORS.
func (s *Server) CORSMiddleware() echo.MiddlewareFunc {
	origins := make([]string, 0, len(s.Config.Cors.AllowedOrigins))
	for _, o := range s.Config.Cors.AllowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     origins,
		AllowMethods:     []string{echo.GET, echo.POST, echo.PUT, echo.DELETE, echo.PATCH, echo.OPTIONS},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, echo.HeaderXCSRFToken, echo.HeaderAccessControlAllowMethods},
		AllowCredentials: true,
	})
}

// isServerAuthEnabled reports whether bearer-token auth is enabled. Auth is
// disabled by default and only turned on when the value is exactly "true".
func isServerAuthEnabled(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "true")
}

func (s *Server) AuthMiddleware() echo.MiddlewareFunc {
	return middleware.KeyAuthWithConfig(middleware.KeyAuthConfig{
		Skipper: func(c echo.Context) bool {
			return !isServerAuthEnabled(s.Config.Server.AuthEnabled)
		},
		KeyLookup: s.Config.Server.AuthKeyLookup,
		Validator: func(auth string, c echo.Context) (bool, error) {
			if len(strings.TrimSpace(s.Config.Server.AuthToken)) == 0 {
				s.logger.Warn("Authentication is enabled but token is not set")
			}
			if strings.Compare(auth, s.Config.Server.AuthToken) == 0 {
				return true, nil
			}
			s.logger.Info("Unauthenticated Request", zap.String("address", c.Request().Host))
			return false, nil
		},
	})
}
