package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Permissions the control endpoint reports on a bucket.
const (
	permRead  = "read"
	permWrite = "write"
	permOwner = "owner"
)

// requireBucketPermission is the server-side authorization gate for AuthModeIAM.
//
// In that mode object calls are signed with a credential the panel obtained on
// the user's behalf, not with keys the user personally holds — so the panel is
// the enforcement point, and a route that forgets to check is a route that
// leaks. This middleware is therefore applied to the whole data-plane group
// rather than to individual handlers: default-deny, and a new route inherits the
// check instead of needing to remember it.
//
// In AuthModeS3 it does nothing: the gateway is still the authority there,
// because every request is signed with the user's own credentials.
func (s *Server) requireBucketPermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !s.Config.Server.IsIAMMode() {
				return next(c)
			}

			bucket := strings.TrimSpace(c.QueryParam("bucket"))
			if bucket == "" {
				// Routes with no bucket parameter (the listing itself) are already
				// scoped to the caller by the control endpoint.
				return next(c)
			}

			buckets, err := s.control.ListBuckets(c.Request().Context(), sessionToken(c))
			if err != nil {
				s.logger.Error("permission check could not reach the control endpoint: " + err.Error())
				return controlError(err, "could not verify bucket access")
			}

			for _, b := range buckets {
				if b.Name != bucket && b.S3Name != bucket && b.RealName != bucket {
					continue
				}
				if b.Can(permission) {
					return next(c)
				}
				return echo.NewHTTPError(http.StatusForbidden,
					"you do not have "+permission+" access to "+bucket)
			}

			// Not in the caller's list. Reported as absent rather than forbidden so
			// the endpoint cannot be used to enumerate buckets.
			return echo.NewHTTPError(http.StatusNotFound, "no such bucket")
		}
	}
}
