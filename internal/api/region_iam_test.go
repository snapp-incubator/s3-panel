package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// multiRegionBucketsXML is a caller with buckets in two regions, one of them
// under the same identifier in both — which is what makes the region part of a
// bucket's identity rather than a detail of it.
const multiRegionBucketsXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>u-alice</ID><DisplayName>Alice</DisplayName></Owner>
  <Buckets>
    <Bucket>
      <Name>payments--shared</Name>
      <BucketRegion>teh-1</BucketRegion>
      <Tenant>payments</Tenant>
      <RealName>shared</RealName>
      <S3Name>payments:shared-teh-1</S3Name>
      <Permissions><Permission>read</Permission></Permissions>
    </Bucket>
    <Bucket>
      <Name>payments--shared</Name>
      <BucketRegion>teh-2</BucketRegion>
      <Tenant>payments</Tenant>
      <RealName>shared</RealName>
      <S3Name>payments:shared-teh-2</S3Name>
      <Permissions><Permission>read</Permission></Permissions>
    </Bucket>
    <Bucket>
      <Name>payments--archive</Name>
      <BucketRegion>teh-2</BucketRegion>
      <Tenant>payments</Tenant>
      <RealName>archive</RealName>
      <S3Name>payments:archive</S3Name>
      <Permissions><Permission>read</Permission></Permissions>
    </Bucket>
    <Bucket>
      <Name>payments--ledger</Name>
      <BucketRegion>teh-1</BucketRegion>
      <Tenant>payments</Tenant>
      <RealName>ledger</RealName>
      <S3Name>payments:ledger</S3Name>
      <Permissions><Permission>read</Permission></Permissions>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

// regionalControl is a control endpoint that answers for every region and
// remembers which region each mint and each stats call was asked for.
type regionalControl struct {
	mu        sync.Mutex
	mints     []string // "<region>|<bucket>"
	statsAsks []string // region header on ?stats
	endpoints map[string]string
}

func (rc *regionalControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	rc.mu.Lock()
	defer rc.mu.Unlock()

	switch {
	case r.Method == http.MethodPost:
		_ = r.ParseForm()
		region := r.FormValue("Region")
		rc.mints = append(rc.mints, region+"|"+r.FormValue("Bucket"))
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<GetSessionTokenResponse><GetSessionTokenResult><Credentials>
  <AccessKeyId>AK-` + region + `</AccessKeyId>
  <SecretAccessKey>SK</SecretAccessKey>
</Credentials><Endpoint>` + rc.endpoints[region] + `</Endpoint><Region>` + region + `</Region></GetSessionTokenResult></GetSessionTokenResponse>`))
	case r.URL.Query().Has("stats"):
		rc.statsAsks = append(rc.statsAsks, r.Header.Get("region"))
		_, _ = w.Write([]byte(`<BucketStats><NumObjects>7</NumObjects></BucketStats>`))
	case r.URL.Query().Has("policy"):
		w.WriteHeader(http.StatusNotFound)
	default:
		_, _ = w.Write([]byte(multiRegionBucketsXML))
	}
}

func (rc *regionalControl) seenMints() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]string(nil), rc.mints...)
}

// regionalServer is an iam-mode panel in teh-1 whose own gateway is
// local-gateway, in front of a control endpoint that also knows teh-2.
func regionalServer(t *testing.T) (*Server, *regionalControl) {
	t.Helper()
	rc := &regionalControl{endpoints: map[string]string{
		"teh-1": "https://s3.teh-1.example",
		"teh-2": "https://s3.teh-2.example",
	}}
	srv := httptest.NewServer(rc)
	t.Cleanup(srv.Close)

	client, err := control.New(srv.URL, srv.URL, time.Second)
	if err != nil {
		t.Fatalf("control.New: %v", err)
	}
	s := &Server{
		Config: config.Config{
			Server:        config.ServerConfig{AuthMode: config.AuthModeIAM, Region: "teh-1"},
			ObjectStorage: config.ObjectStorageConfig{URL: "https://local-gateway.example"},
		},
		control:     client,
		logger:      zap.NewNop(),
		credentials: newCredentialCache(t.Context()),
	}
	if err := s.buildRegionTargets(); err != nil {
		t.Fatalf("buildRegionTargets: %v", err)
	}
	return s, rc
}

// objectCall runs the gateway chain for one read request and reports what the
// handler would sign with: the bucket it was handed and the gateway it would
// call.
type objectCall struct {
	status  int
	bucket  string
	gateway string
}

func runObjectCall(t *testing.T, s *Server, bucket, region string) objectCall {
	t.Helper()
	var got objectCall
	handler := func(c echo.Context) error {
		got.bucket = c.QueryParam("bucket")
		got.gateway = s.gatewayConfig(c).URL
		return c.NoContent(http.StatusOK)
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/object/list?bucket="+bucket, nil)
	if region != "" {
		req.Header.Set("region", region)
	}
	c := withSession(e.NewContext(req, httptest.NewRecorder()))

	chain := s.requireBucketPermission(permRead)(s.injectObjectCredentials()(handler))
	got.status = http.StatusOK
	if err := chain(c); err != nil {
		he, ok := err.(*echo.HTTPError)
		if !ok {
			t.Fatalf("unexpected error: %v", err)
		}
		got.status = he.Code
	}
	return got
}

// TestBucketResolvesInTheRegionTheCallerNamed: two regions hold a bucket under
// the same identifier, and the one the caller opened is the one they get —
// not whichever the control endpoint happened to list first.
func TestBucketResolvesInTheRegionTheCallerNamed(t *testing.T) {
	s, rc := regionalServer(t)

	got := runObjectCall(t, s, "payments--shared", "teh-2")

	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if got.bucket != "payments:shared-teh-2" {
		t.Errorf("handler got bucket %q, want the teh-2 one", got.bucket)
	}
	if mints := rc.seenMints(); len(mints) != 1 || mints[0] != "teh-2|payments--shared" {
		t.Errorf("minted %v, want [teh-2|payments--shared]", mints)
	}
}

// TestBucketInAnotherRegionIsNotResolvedFromThisOne: naming a region narrows the
// match, it does not fall back to another region's bucket.
func TestBucketInAnotherRegionIsNotResolvedFromThisOne(t *testing.T) {
	s, rc := regionalServer(t)

	got := runObjectCall(t, s, "payments--archive", "teh-1")

	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404: archive lives in teh-2 only", got.status)
	}
	if mints := rc.seenMints(); len(mints) != 0 {
		t.Errorf("minted %v for a bucket that was not resolved", mints)
	}
}

// TestCredentialIsMintedInTheBucketsOwnRegion is the bug: every object call was
// minted for the region in the request header, which the browser always set to
// its default — so a bucket in any other region was refused by the control
// endpoint ("You have no access to bucket ...") and its objects never listed.
func TestCredentialIsMintedInTheBucketsOwnRegion(t *testing.T) {
	s, rc := regionalServer(t)

	got := runObjectCall(t, s, "payments--archive", "")

	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if mints := rc.seenMints(); len(mints) != 1 || mints[0] != "teh-2|payments--archive" {
		t.Errorf("minted %v, want [teh-2|payments--archive] — the bucket's region, not this instance's", mints)
	}
}

// TestObjectCallsGoToTheBucketsRegionalGateway: a bucket in another region does
// not exist on this instance's gateway, so its object calls must go to the
// gateway the control endpoint named for that region.
func TestObjectCallsGoToTheBucketsRegionalGateway(t *testing.T) {
	s, _ := regionalServer(t)

	if got := runObjectCall(t, s, "payments--archive", "teh-2"); got.gateway != "https://s3.teh-2.example" {
		t.Errorf("teh-2 bucket signed against %q, want the teh-2 gateway", got.gateway)
	}
	// This instance's own region keeps the gateway it is configured with.
	if got := runObjectCall(t, s, "payments--ledger", "teh-1"); got.gateway != "https://local-gateway.example" {
		t.Errorf("teh-1 bucket signed against %q, want the configured gateway", got.gateway)
	}
}

// TestRegionalGatewayFallsBackToTheConfiguredOne: a control endpoint that names
// no endpoint leaves the configured gateway in place.
func TestRegionalGatewayFallsBackToTheConfiguredOne(t *testing.T) {
	s, rc := regionalServer(t)
	rc.endpoints = map[string]string{}

	if got := runObjectCall(t, s, "payments--archive", "teh-2"); got.gateway != "https://local-gateway.example" {
		t.Errorf("gateway = %q, want the configured one", got.gateway)
	}
}

// TestIAMModeServesEveryRegionItself: the control endpoint answers for every
// region and the credential names each region's gateway, so a request for a
// remote region is not proxied — nor refused for want of a configured upstream.
func TestIAMModeServesEveryRegionItself(t *testing.T) {
	s, _ := regionalServer(t)

	rec := doGet(regionEcho(s), "/api/bucket/list", "teh-2")

	if rec.Code != http.StatusOK || rec.Body.String() != "local" {
		t.Fatalf("got %d %q, want 200 local", rec.Code, rec.Body.String())
	}
}

func listBuckets(t *testing.T, s *Server, query string) IAMBucketListResponse {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	c := withSession(e.NewContext(httptest.NewRequest(http.MethodGet, "/bucket/list?"+query, nil), rec))
	if err := s.HandleIAMBucketList()(c); err != nil {
		t.Fatalf("list: %v", err)
	}
	var out IAMBucketListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// TestBucketListFiltersByRegion: the filter narrows the items and the paging,
// but not the region choices — those stay every region the caller has buckets
// in, or picking one would leave nothing to switch back to.
func TestBucketListFiltersByRegion(t *testing.T) {
	s, _ := regionalServer(t)

	out := listBuckets(t, s, "region=teh-2&max_keys=10&page=1")

	var got []string
	for _, b := range out.Items {
		got = append(got, b.Region+"/"+b.Bucket)
	}
	if strings.Join(got, ",") != "teh-2/payments--shared,teh-2/payments--archive" {
		t.Errorf("items = %v, want only teh-2 buckets", got)
	}
	if out.TotalBuckets != 2 {
		t.Errorf("total_buckets = %d, want 2", out.TotalBuckets)
	}
	if strings.Join(out.Regions, ",") != "teh-1,teh-2" {
		t.Errorf("regions = %v, want every region the caller has buckets in", out.Regions)
	}

	// Search narrows the choices no more than the filter does.
	if out := listBuckets(t, s, "search_string=ledger"); strings.Join(out.Regions, ",") != "teh-1,teh-2" {
		t.Errorf("regions under search = %v", out.Regions)
	}
}

// TestBucketDetailAsksTheBucketsOwnRegion: the stats of a teh-2 bucket come from
// teh-2 even when the request names no region.
func TestBucketDetailAsksTheBucketsOwnRegion(t *testing.T) {
	s, rc := regionalServer(t)

	e := echo.New()
	rec := httptest.NewRecorder()
	c := withSession(e.NewContext(httptest.NewRequest(http.MethodGet, "/bucket/detail?bucket=payments--archive", nil), rec))
	if err := s.HandleIAMBucketDetail()(c); err != nil {
		t.Fatalf("detail: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(rc.statsAsks) != 1 || rc.statsAsks[0] != "teh-2" {
		t.Errorf("stats asked of %v, want [teh-2]", rc.statsAsks)
	}

	// And of two same-named buckets, the one in the region the caller named.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bucket/detail?bucket=payments--shared", nil)
	req.Header.Set("region", "teh-2")
	c = withSession(e.NewContext(req, rec))
	if err := s.HandleIAMBucketDetail()(c); err != nil {
		t.Fatalf("detail: %v", err)
	}
	var detail IAMBucketDetailResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.Region != "teh-2" || detail.S3Name != "payments:shared-teh-2" {
		t.Errorf("detail resolved %s/%s, want teh-2/payments:shared-teh-2", detail.Region, detail.S3Name)
	}
}
