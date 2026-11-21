package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// iamBucketList is what IAM's ListBuckets answers (internal/awss3): Name is
// the resource name cephname.Join builds, "<tenant>--<bucket>", or the bare
// name of an untenanted bucket. The same tenanted name exists in two regions.
const iamBucketList = `<ListAllMyBucketsResult><Buckets>` +
	`<Bucket><Name>okd4_teh_1__payments--exports</Name><BucketRegion>teh-1</BucketRegion>` +
	`<Tenant>okd4_teh_1__payments</Tenant><RealName>exports</RealName>` +
	`<ResourceID>ceph-teh-1|bucket|okd4_teh_1__payments--exports</ResourceID></Bucket>` +
	`<Bucket><Name>okd4_teh_1__payments--exports</Name><BucketRegion>teh-2</BucketRegion>` +
	`<Tenant>okd4_teh_1__payments</Tenant><RealName>exports</RealName>` +
	`<ResourceID>ceph-teh-2|bucket|okd4_teh_1__payments--exports</ResourceID></Bucket>` +
	`<Bucket><Name>legacy-reports</Name><BucketRegion>teh-1</BucketRegion>` +
	`<ResourceID>ceph-teh-1|bucket|legacy-reports</ResourceID></Bucket>` +
	`<Bucket><Name>dup</Name><BucketRegion>teh-1</BucketRegion></Bucket>` +
	`<Bucket><Name>dup</Name><BucketRegion>us-teh-1</BucketRegion></Bucket>` +
	`</Buckets></ListAllMyBucketsResult>`

func resolverServer(t *testing.T, base string) *Server {
	t.Helper()
	ctl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(iamBucketList))
	}))
	t.Cleanup(ctl.Close)
	s, _ := actAsServer(t)
	s.Config.Server.BasePath = base
	s.auth.BasePath = base
	client, err := control.New(ctl.URL+"/s3", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s.control = client
	s.registerRouter()
	s.registerRoutes()
	return s
}

// TestResolveIAMBucketNames: every spelling a console link may carry for a
// bucket, tenanted or not, lands on the panel's own page for it, in its own
// region.
func TestResolveIAMBucketNames(t *testing.T) {
	s := resolverServer(t, "/storage")

	const tenanted = "/storage/object-storage/s3-bucket/buckets/okd4_teh_1__payments--exports?region="
	for link, want := range map[string]string{
		// IAM's resource name, as the header percent-encodes it.
		"/storage/iam/buckets/okd4_teh_1__payments--exports?region=teh-1": tenanted + "teh-1",
		"/storage/iam/buckets/okd4_teh_1__payments--exports?region=teh-2": tenanted + "teh-2",
		// The region as the service, or as a catalog name ending in it.
		"/storage/iam/buckets/okd4_teh_1__payments--exports?region=ceph-teh-2": tenanted + "teh-2",
		"/storage/iam/buckets/okd4_teh_1__payments--exports?region=okd4-teh-2": tenanted + "teh-2",
		// No region: the first match.
		"/storage/iam/buckets/okd4_teh_1__payments--exports": tenanted + "teh-1",
		// A region holding no bucket of that name is not swapped for another
		// region's namesake, whether the person knows the region or not: the
		// page then answers not found.
		"/storage/iam/buckets/okd4_teh_1__payments--exports?region=teh-3":                           tenanted + "teh-3",
		"/storage/iam/buckets/okd4_teh_1__payments--exports?region=mars-9":                          tenanted + "mars-9",
		"/storage/iam/buckets/" + url.PathEscape("ceph-teh-3|bucket|okd4_teh_1__payments--exports"): tenanted + "ceph-teh-3",
		// An unknown spelling is ignored only when the name alone is unique.
		"/storage/iam/buckets/legacy-reports?region=mars-9": "/storage/object-storage/s3-bucket/buckets/legacy-reports?region=teh-1",
		// An exact region wins over a "<prefix>-<region>" one.
		"/storage/iam/buckets/dup?region=us-teh-1": "/storage/object-storage/s3-bucket/buckets/dup?region=us-teh-1",
		"/storage/iam/buckets/dup?region=teh-1":    "/storage/object-storage/s3-bucket/buckets/dup?region=teh-1",
		// Decoded once: "%25" stays a literal "%".
		"/storage/iam/buckets/a%2525b": "/storage/object-storage/s3-bucket/buckets/a%2525b",
		// Dot segments are not names.
		"/storage/iam/buckets/..":                                        "/storage/object-storage/s3-bucket/buckets/",
		"/storage/iam/buckets/..%2F..%2Fiam":                             "/storage/object-storage/s3-bucket/buckets/",
		"/storage/iam/buckets/team%2F..":                                 "/storage/object-storage/s3-bucket/buckets/",
		"/storage/iam/buckets/..:x":                                      "/storage/object-storage/s3-bucket/buckets/",
		"/storage/iam/buckets/a%2Fb%2Fc":                                 "/storage/object-storage/s3-bucket/buckets/",
		"/storage/iam/buckets/" + url.PathEscape("ceph-teh-1|bucket|.."): "/storage/object-storage/s3-bucket/buckets/",
		// RGW's spellings, encoded or not.
		"/storage/iam/buckets/okd4_teh_1__payments%2Fexports?region=teh-2": tenanted + "teh-2",
		"/storage/iam/buckets/okd4_teh_1__payments:exports":                tenanted + "teh-1",
		// A whole resource ID: its service names the region.
		"/storage/iam/buckets/" + url.PathEscape("ceph-teh-2|bucket|okd4_teh_1__payments--exports"): tenanted + "teh-2",
		// Untenanted.
		"/storage/iam/buckets/legacy-reports?region=ceph-teh-1": "/storage/object-storage/s3-bucket/buckets/legacy-reports?region=teh-1",
		// Not visible to this person: the page answers not found, as for a
		// bucket that does not exist.
		"/storage/iam/buckets/someone--else?region=teh-1": "/storage/object-storage/s3-bucket/buckets/someone--else?region=teh-1",
	} {
		rec := serve(s.Router, signedInRequest(t, s.auth, link, "u-alice"))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s -> %d %q, want 302 %q", link, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestResolveSendsAnonymousVisitorsToSignIn(t *testing.T) {
	e := resolverServer(t, "/storage").Router
	link := "/storage/iam/buckets/okd4_teh_1__payments--exports?region=teh-1"
	rec := serve(e, httptest.NewRequest(http.MethodGet, link, nil))
	want := "/storage/auth/login?next=" + url.QueryEscape(link)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
		t.Errorf("anonymous -> %d %q, want 302 %q", rec.Code, rec.Header().Get("Location"), want)
	}
}

func TestResolveRespectsTheViewGate(t *testing.T) {
	s, _ := actAsServer(t)
	s.Config.Panel = config.PanelConfig{Enabled: true}.WithDefaults()
	s.view = newViewGate((&console{view: map[string]bool{}}).ask)
	s.registerRouter()
	s.registerRoutes()
	rec := serve(s.Router, signedInRequest(t, s.auth, "/iam/buckets/x--y", "u-bob"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-viewer -> %d, want the No access page", rec.Code)
	}
}

func TestNormaliseBucketName(t *testing.T) {
	for in, want := range map[string]string{
		"t--b": "t--b", "t/b": "t--b", "t:b": "t--b", "bare": "bare",
		"/b": "/b", "t/": "t/", "okd4_teh_1__payments--exports": "okd4_teh_1__payments--exports",
	} {
		if got := normaliseBucketName(in); got != want {
			t.Errorf("normaliseBucketName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStandaloneSPAKeepsIAMPaths: the resolver exists only in iam mode, so in
// the standalone s3 mode the SPA still answers /iam/buckets/... as before.
func TestStandaloneSPAKeepsIAMPaths(t *testing.T) {
	s := &Server{Config: config.DefaultConfig(), logger: zap.NewNop()}
	s.registerRouter()
	s.registerRoutes()
	rec := serve(s.Router, httptest.NewRequest(http.MethodGet, "/iam/buckets/x", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("s3 mode /iam/buckets/x = %d, want the SPA", rec.Code)
	}
}
