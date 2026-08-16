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
				if !b.Can(permission) {
					return echo.NewHTTPError(http.StatusForbidden,
						"you do not have "+permission+" access to "+bucket)
				}
				// Rewrite the bucket to the gateway's own spelling before the
				// handler binds it.
				//
				// The panel addresses a bucket by its control-plane identifier
				// ("<tenant>--<bucket>"), which is unique across regions and
				// URL-safe. The gateway has never heard of that name — it wants
				// "<tenant>:<bucket>" — so handing the identifier straight to the
				// S3 client makes every object call fail with NoSuchBucket.
				//
				// Done here because this is where the bucket has just been
				// resolved: one lookup answers both "may you?" and "what is it
				// actually called?", and no handler has to remember either.
				if b.S3Name != "" && b.S3Name != bucket {
					rewriteBucketParam(c, b.S3Name)
				}
				return next(c)
			}

			// Not in the caller's list. Reported as absent rather than forbidden so
			// the endpoint cannot be used to enumerate buckets.
			return echo.NewHTTPError(http.StatusNotFound, "no such bucket")
		}
	}
}

// rewriteBucketParam replaces the request's bucket parameter in place.
//
// It rewrites both the URL query and, for multipart uploads, the parsed form —
// the upload handler binds `bucket` from the form rather than the query, so
// changing only one of them fixes listing and leaves uploads broken.
func rewriteBucketParam(c echo.Context, name string) {
	req := c.Request()

	// Echo memoizes the parsed query on first access, and this middleware has
	// already read `bucket` to resolve it — so rewriting only RawQuery leaves
	// every later QueryParam call returning the stale value. Update the cached
	// values too, and keep RawQuery coherent for anything reading the raw URL.
	if q := c.QueryParams(); q.Has("bucket") {
		q.Set("bucket", name)
		req.URL.RawQuery = q.Encode()
	}

	// Only touch the form if it has already been parsed; forcing a parse here
	// would consume a multipart body the handler still needs to read.
	if req.PostForm != nil && req.PostForm.Has("bucket") {
		req.PostForm.Set("bucket", name)
	}
	if req.MultipartForm != nil && req.MultipartForm.Value != nil {
		if _, ok := req.MultipartForm.Value["bucket"]; ok {
			req.MultipartForm.Value["bucket"] = []string{name}
		}
	}
}
