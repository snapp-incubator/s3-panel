package api

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/control"
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

			bucket := bucketParam(c)
			if bucket == "" {
				// Routes that genuinely name no bucket (the listing itself) are
				// already scoped to the caller by the control endpoint.
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

				// Hand the resolved bucket to whatever runs next. The credential
				// middleware needs its tenant, and this lookup already paid for the
				// answer — repeating it there would mean a second round trip to the
				// control endpoint on every object request.
				c.Set(resolvedBucketKey, b)

				// Record what this ROUTE needs, not what the user could do. A
				// listing asks for read even from someone who may write the bucket,
				// so the credential it is signed with cannot write — the permission
				// gate stops being the only thing between a bug and a mutation.
				c.Set(requiredPermissionKey, permission)
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

	// An upload arrives as multipart/form-data that nothing has parsed yet, so
	// waiting for "already parsed" means never rewriting it at all: the handler
	// then parses the raw body itself and binds the ORIGINAL control-plane name,
	// which the gateway does not know — the upload fails with NoSuchBucket while
	// listing the same bucket works.
	//
	// Parsing here is safe rather than destructive: ParseMultipartForm is
	// idempotent, so the handler's own c.MultipartForm() and c.FormValue() reuse
	// exactly what this parse produced instead of re-reading a consumed body.
	if isMultipartForm(req) {
		_ = req.ParseMultipartForm(multipartParseMemory)
	}

	// c.FormValue reads Form, the binder reads PostForm, and the file walk reads
	// MultipartForm. All three are populated from the same body and any one of
	// them can be the copy the handler happens to consult, so rewrite each.
	for _, values := range []url.Values{req.Form, req.PostForm} {
		if values != nil && values.Has("bucket") {
			values.Set("bucket", name)
		}
	}
	if req.MultipartForm != nil && req.MultipartForm.Value != nil {
		if _, ok := req.MultipartForm.Value["bucket"]; ok {
			req.MultipartForm.Value["bucket"] = []string{name}
		}
	}
}

// multipartParseMemory is how much of an upload is held in memory before the
// rest spills to temporary files. It matches Echo's own default so parsing early
// behaves exactly as the handler's later parse would have.
const multipartParseMemory = 32 << 20

// isMultipartForm reports whether the body is a multipart form, without reading
// it.
func isMultipartForm(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "multipart/form-data"
}

// bucketParam reads the bucket a request targets, from wherever its route
// carries it.
//
// Browsing routes put it in the query string; an upload puts it in a multipart
// form field. Reading only the query returned "" for uploads, which sent this
// middleware down its "names no bucket, nothing to check" path — so an upload
// skipped the permission check ENTIRELY and never had its bucket rewritten to
// the gateway spelling. The failed upload was the visible half of that; the
// unchecked write was the dangerous half.
func bucketParam(c echo.Context) string {
	if v := strings.TrimSpace(c.QueryParam("bucket")); v != "" {
		return v
	}

	// Only touch the body when it actually is a form. Calling FormValue on a JSON
	// or empty body would be pointless here and needlessly parse it.
	req := c.Request()
	switch {
	case isMultipartForm(req):
		_ = req.ParseMultipartForm(multipartParseMemory)
	case isURLEncodedForm(req):
		_ = req.ParseForm()
	default:
		return ""
	}
	return strings.TrimSpace(c.FormValue("bucket"))
}

// isURLEncodedForm reports whether the body is a plain form post.
func isURLEncodedForm(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "application/x-www-form-urlencoded"
}

// resolvedBucketKey is where requireBucketPermission leaves the bucket it
// resolved, for later middleware in the same chain.
const resolvedBucketKey = "iam.resolved_bucket"

// resolvedBucket returns the bucket requireBucketPermission resolved for this
// request, if it ran and found one.
func resolvedBucket(c echo.Context) (control.Bucket, bool) {
	b, ok := c.Get(resolvedBucketKey).(control.Bucket)
	return b, ok
}

// requiredPermissionKey is where requireBucketPermission leaves the permission
// the ROUTE declared, for the credential middleware in the same chain.
const requiredPermissionKey = "iam.required_permission"

// Storage access levels the control endpoint understands. They are coarser than
// the panel's permissions because RGW has only these three, and they are
// account-wide.
const (
	accessRead      = "read"
	accessReadWrite = "readwrite"
)

// requiredAccess is the storage access level this request needs, derived from
// the permission its route declared.
//
// Defaults to read whenever the route named no permission — a route that reaches
// the gateway without going through the permission gate should not be able to
// obtain a credential that can mutate anything.
func requiredAccess(c echo.Context) string {
	perm, _ := c.Get(requiredPermissionKey).(string)
	switch perm {
	case permWrite, permOwner:
		return accessReadWrite
	default:
		return accessRead
	}
}
