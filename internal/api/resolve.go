package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// bucketPagePath is the SPA's page for one bucket, under the base path.
const bucketPagePath = "/object-storage/s3-bucket/buckets/"

// handleResolveIAMBucket serves GET <base>/iam/buckets/{name}?region={region}:
// it takes the control endpoint's name for a bucket and redirects to this
// panel's page for it.
//
// It is the target a unified console's links point at, so the console can
// build a link from a catalog resource without knowing how the panel spells
// buckets. It accepts every spelling a link may carry:
//
//   - the resource name, "<tenant>--<bucket>", or the bare name of an
//     untenanted bucket;
//   - RGW's spellings, "<tenant>/<bucket>" and "<tenant>:<bucket>";
//   - a whole resource ID, "<service>|bucket|<name>".
//
// and a region in any form the console uses: the bucket's own region, or the
// service that holds it ("ceph-teh-1"), or a catalog name ending in it
// ("okd4-teh-1").
//
// The bucket is looked up as the signed-in person, so the redirect carries its
// real name and region. One the person cannot see redirects all the same, with
// the name as given, and the page then answers "not found" exactly as it would
// for a bucket that does not exist. A visitor who is not signed in is sent to
// sign in first, and returns here.
func (s *Server) handleResolveIAMBucket() echo.HandlerFunc {
	return func(c echo.Context) error {
		base := s.Config.Server.Base()
		// Echo hands over the raw (still escaped) value only when the path had
		// to be matched raw, i.e. it carried an escaped "/"; otherwise it is
		// already decoded, and decoding it again would turn "%25" into "%".
		raw := c.Param("*")
		if c.Request().URL.RawPath != "" {
			if unescaped, err := url.PathUnescape(raw); err == nil {
				raw = unescaped
			}
		}
		service, name := splitResourceID(strings.TrimSpace(raw))
		if !plausibleBucketName(name) {
			return c.Redirect(http.StatusFound, base+bucketPagePath)
		}
		name = normaliseBucketName(name)
		region := strings.TrimSpace(c.QueryParam("region"))

		session, ok := s.auth.Load(c)
		if !ok {
			next := c.Request().URL.RequestURI()
			return c.Redirect(http.StatusFound, base+"/auth/login?next="+url.QueryEscape(next))
		}
		if s.view != nil {
			if verdict := s.view.check(c.Request().Context(), session.Subject); verdict != viewAllowed {
				return s.renderNoAccess(c, verdict, session.Subject)
			}
		}

		target, targetRegion := name, region
		ctx := control.WithActAs(c.Request().Context(), session.ActAs)
		buckets, err := s.control.ListBuckets(ctx, session.AccessToken)
		if err != nil {
			// Fails safe: the name goes through as given, and the page asks again.
			s.logger.Warn("resolver could not list the person's buckets", zap.Error(err))
		} else if b := matchIAMBucket(buckets, name, service, region); b != nil {
			target, targetRegion = b.Name, b.BucketRegion
		}
		if targetRegion == "" {
			// Unresolved, the service still says which region was meant, so the
			// page does not pick a same-named bucket from another one.
			targetRegion = service
		}

		dest := base + bucketPagePath + url.PathEscape(target)
		if targetRegion != "" {
			dest += "?region=" + url.QueryEscape(targetRegion)
		}
		return c.Redirect(http.StatusFound, dest)
	}
}

// splitResourceID separates "<service>|bucket|<name>" into its service and
// name. Anything else is a bare name.
func splitResourceID(s string) (service, name string) {
	parts := strings.Split(s, "|")
	if len(parts) == 3 && parts[1] == "bucket" {
		return parts[0], parts[2]
	}
	return "", s
}

// plausibleBucketName reports whether name can name a bucket at all, in any of
// the spellings the resolver accepts. It refuses an empty name, a "." or ".."
// in any segment ("../../iam" would otherwise become "..--../iam"), and more
// than one tenant separator: a bucket name holds no "/" or ":" of its own.
func plausibleBucketName(name string) bool {
	segments := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == ':' })
	if len(segments) == 0 || len(segments) > 2 || strings.Count(name, "/")+strings.Count(name, ":") > 1 {
		return false
	}
	for _, seg := range segments {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// normaliseBucketName turns RGW's "<tenant>/<bucket>" and "<tenant>:<bucket>"
// into the control endpoint's "<tenant>--<bucket>" (IAM's cephname.Join).
// Bucket names cannot contain "/" or ":", so the first one is the tenant's end.
func normaliseBucketName(name string) string {
	if i := strings.IndexAny(name, "/:"); i > 0 && i < len(name)-1 {
		return name[:i] + "--" + name[i+1:]
	}
	return name
}

// matchIAMBucket picks the bucket a link names out of the person's list.
//
// A service in a resource ID names its region precisely, and is never
// overridden: a bucket of that name in another region is a DIFFERENT bucket,
// so no match there leaves the link unresolved (the page then answers "not
// found", as requireBucketPermission does for a region mismatch). A region
// parameter is honoured the same way when it names a region the person has
// any bucket in; only a spelling that names none of them is ignored, and then
// only when the name alone is unambiguous.
func matchIAMBucket(buckets []control.Bucket, name, service, region string) *control.Bucket {
	var candidates []*control.Bucket
	for i := range buckets {
		if buckets[i].Name == name {
			candidates = append(candidates, &buckets[i])
		}
	}
	if service != "" {
		return pickRegion(candidates, service)
	}
	if region != "" {
		if b := pickRegion(candidates, region); b != nil {
			return b
		}
		for i := range buckets {
			if regionMatchesExactly(buckets[i], region) || regionMatchesBySuffix(buckets[i], region) {
				// A region the person knows, holding no bucket of this name.
				return nil
			}
		}
	}
	if len(candidates) == 1 || (region == "" && len(candidates) > 0) {
		return candidates[0]
	}
	return nil
}

// pickRegion returns the candidate in the named region, preferring an exact
// match over a "<prefix>-<region>" one, so "us-teh-1" can never pick teh-1 when
// us-teh-1 itself is there.
func pickRegion(candidates []*control.Bucket, want string) *control.Bucket {
	for _, b := range candidates {
		if regionMatchesExactly(*b, want) {
			return b
		}
	}
	for _, b := range candidates {
		if regionMatchesBySuffix(*b, want) {
			return b
		}
	}
	return nil
}

// regionMatchesExactly reports whether want is the bucket's region, or the
// service in its resource ID ("ceph-teh-1").
func regionMatchesExactly(b control.Bucket, want string) bool {
	if b.BucketRegion != "" && strings.EqualFold(want, b.BucketRegion) {
		return true
	}
	service, _, _ := strings.Cut(b.ResourceID, "|")
	return service != "" && strings.EqualFold(want, service)
}

// regionMatchesBySuffix reports whether want is a "<prefix>-<region>" name for
// the bucket's region, such as the catalog's "okd4-teh-1".
func regionMatchesBySuffix(b control.Bucket, want string) bool {
	return b.BucketRegion != "" && strings.HasSuffix(strings.ToLower(want), "-"+strings.ToLower(b.BucketRegion))
}
