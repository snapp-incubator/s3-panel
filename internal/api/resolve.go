package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

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
		raw := c.Param("*")
		if unescaped, err := url.PathUnescape(raw); err == nil {
			raw = unescaped
		}
		service, name := splitResourceID(strings.TrimSpace(raw))
		name = normaliseBucketName(name)
		region := strings.TrimSpace(c.QueryParam("region"))
		if name == "" {
			return c.Redirect(http.StatusFound, base+bucketPagePath)
		}

		session, ok := s.auth.Load(c)
		if !ok {
			next := c.Request().URL.RequestURI()
			return c.Redirect(http.StatusFound, base+"/auth/login?next="+url.QueryEscape(next))
		}
		if s.view != nil {
			if verdict := s.view.check(c.Request().Context(), session.Subject); verdict != viewAllowed {
				return s.renderNoAccess(c, verdict)
			}
		}

		target, targetRegion := name, region
		ctx := control.WithActAs(c.Request().Context(), session.ActAs)
		if buckets, err := s.control.ListBuckets(ctx, session.AccessToken); err == nil {
			if b := matchIAMBucket(buckets, name, service, region); b != nil {
				target, targetRegion = b.Name, b.BucketRegion
			}
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

// normaliseBucketName turns RGW's "<tenant>/<bucket>" and "<tenant>:<bucket>"
// into the control endpoint's "<tenant>--<bucket>" (IAM's cephname.Join).
// Bucket names cannot contain "/" or ":", so the first one is the tenant's end.
func normaliseBucketName(name string) string {
	if i := strings.IndexAny(name, "/:"); i > 0 && i < len(name)-1 {
		return name[:i] + "--" + name[i+1:]
	}
	return name
}

// matchIAMBucket picks the bucket a link names out of the person's list. A
// region that matches none of the candidates is ignored rather than trusted,
// so a link in an unexpected region spelling still lands on the bucket when
// its name is unique.
func matchIAMBucket(buckets []control.Bucket, name, service, region string) *control.Bucket {
	var candidates []*control.Bucket
	for i := range buckets {
		if buckets[i].Name == name {
			candidates = append(candidates, &buckets[i])
		}
	}
	for _, want := range []string{service, region} {
		if want == "" {
			continue
		}
		for _, b := range candidates {
			if regionMatches(*b, want) {
				return b
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}

// regionMatches reports whether a region or service spelling names the
// bucket's region: exactly, as the service in its resource ID, or as a
// "<prefix>-<region>" name such as "ceph-teh-1" or "okd4-teh-1".
func regionMatches(b control.Bucket, want string) bool {
	if b.BucketRegion != "" && (strings.EqualFold(want, b.BucketRegion) ||
		strings.HasSuffix(strings.ToLower(want), "-"+strings.ToLower(b.BucketRegion))) {
		return true
	}
	service, _, _ := strings.Cut(b.ResourceID, "|")
	return service != "" && strings.EqualFold(want, service)
}
