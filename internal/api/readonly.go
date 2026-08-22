package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// refuseWhenReadOnly rejects any request that would modify the storage backend.
//
// This is a deployment-level statement, not an authorization check, and it
// deliberately outranks both. A user may legitimately hold write or owner on a
// bucket — the permission gate would let them through, and in iam mode the panel
// holds a credential that can act on it. When the instance is pointed at storage
// it must not change (a staging panel reading a production gateway, an audit
// console), that is exactly the situation this guards.
//
// Applied as middleware on the mutating routes rather than checked inside each
// handler, so a route added later cannot quietly become writable.
func (s *Server) refuseWhenReadOnly() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !s.Config.Server.IsReadOnly() {
				return next(c)
			}
			return echo.NewHTTPError(http.StatusForbidden,
				"this panel is read-only; it cannot modify the storage backend")
		}
	}
}
