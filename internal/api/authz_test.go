package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// listBucketsXML is one tenanted bucket as a control endpoint reports it: the
// control-plane identifier in <Name>, the gateway's own spelling in <S3Name>.
const listBucketsXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>u-alice</ID><DisplayName>Alice</DisplayName></Owner>
  <Buckets>
    <Bucket>
      <Name>okd4_teh_1__payments--exports</Name>
      <BucketRegion>teh-1</BucketRegion>
      <Tenant>okd4_teh_1__payments</Tenant>
      <RealName>exports</RealName>
      <S3Name>okd4_teh_1__payments:exports</S3Name>
      <Permissions><Permission>read</Permission></Permissions>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

func iamModeServer(t *testing.T) (*Server, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(listBucketsXML))
	}))
	client, err := control.New(srv.URL, srv.URL, time.Second)
	if err != nil {
		t.Fatalf("control.New: %v", err)
	}
	return &Server{
		Config:  config.Config{Server: config.ServerConfig{AuthMode: config.AuthModeIAM}},
		control: client,
		logger:  zap.NewNop(),
	}, srv.Close
}

func withSession(c echo.Context) echo.Context {
	c.Set(auth.SessionContextKey, auth.Session{Subject: "u-alice", Email: "alice@example.com"})
	return c
}

// TestBucketParamIsRewrittenToGatewayName is the regression test for the reason
// object operations returned NoSuchBucket for every tenanted bucket.
//
// The panel addresses buckets by the control-plane identifier
// ("<tenant>--<bucket>"), which is what the UI navigates with. The gateway wants
// "<tenant>:<bucket>". Handing the identifier to the S3 client makes listing,
// download and upload all fail — on every operator-provisioned bucket, which is
// all of them.
func TestBucketParamIsRewrittenToGatewayName(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	var seen string
	handler := func(c echo.Context) error {
		seen = c.QueryParam("bucket")
		return c.NoContent(http.StatusOK)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/object/list?bucket=okd4_teh_1__payments--exports&page=1", nil)
	c := withSession(e.NewContext(req, httptest.NewRecorder()))

	if err := s.requireBucketPermission(permRead)(handler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "okd4_teh_1__payments:exports"; seen != want {
		t.Errorf("handler received bucket %q, want %q — the gateway has no bucket by the control-plane name", seen, want)
	}
}

// TestRewritePreservesOtherQueryParams: the rewrite re-encodes the query, so
// everything else the handler binds has to survive it.
func TestRewritePreservesOtherQueryParams(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	var page, prefix string
	handler := func(c echo.Context) error {
		page, prefix = c.QueryParam("page"), c.QueryParam("prefix")
		return c.NoContent(http.StatusOK)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet,
		"/object/list?bucket=okd4_teh_1__payments--exports&page=3&prefix=data/2026", nil)
	c := withSession(e.NewContext(req, httptest.NewRecorder()))

	if err := s.requireBucketPermission(permRead)(handler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != "3" || prefix != "data/2026" {
		t.Errorf("query lost in rewrite: page=%q prefix=%q", page, prefix)
	}
}

// TestUntenantedBucketIsUnchanged: an untenanted bucket has the same name on
// both sides, so nothing should be rewritten.
func TestUntenantedBucketIsUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<ListAllMyBucketsResult><Buckets><Bucket>
  <Name>public-assets</Name><RealName>public-assets</RealName>
  <S3Name>public-assets</S3Name>
  <Permissions><Permission>read</Permission></Permissions>
</Bucket></Buckets></ListAllMyBucketsResult>`))
	}))
	defer srv.Close()
	client, _ := control.New(srv.URL, srv.URL, time.Second)
	s := &Server{
		Config:  config.Config{Server: config.ServerConfig{AuthMode: config.AuthModeIAM}},
		control: client, logger: zap.NewNop(),
	}

	var seen string
	handler := func(c echo.Context) error { seen = c.QueryParam("bucket"); return c.NoContent(http.StatusOK) }

	e := echo.New()
	c := withSession(e.NewContext(
		httptest.NewRequest(http.MethodGet, "/object/list?bucket=public-assets", nil), httptest.NewRecorder()))

	if err := s.requireBucketPermission(permRead)(handler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen != "public-assets" {
		t.Errorf("bucket = %q, want it untouched", seen)
	}
}

// TestPermissionDenialStillWins: the rewrite must not become a way in. A bucket
// the caller lacks the permission for is refused before any rewrite happens.
func TestPermissionDenialStillWins(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	reached := false
	handler := func(c echo.Context) error { reached = true; return c.NoContent(http.StatusOK) }

	e := echo.New()
	c := withSession(e.NewContext(
		httptest.NewRequest(http.MethodPost, "/object/upload?bucket=okd4_teh_1__payments--exports", nil),
		httptest.NewRecorder()))

	// The fixture grants read only.
	err := s.requireBucketPermission(permWrite)(handler)(c)
	if reached {
		t.Fatal("handler ran without the required permission")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusForbidden {
		t.Fatalf("error = %v, want 403", err)
	}
}

// TestUnknownBucketReadsAsAbsent: telling "forbidden" apart from "absent" would
// let any caller enumerate buckets they cannot see.
func TestUnknownBucketReadsAsAbsent(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	e := echo.New()
	c := withSession(e.NewContext(
		httptest.NewRequest(http.MethodGet, "/object/list?bucket=someone-elses-bucket", nil),
		httptest.NewRecorder()))

	err := s.requireBucketPermission(permRead)(func(echo.Context) error { return nil })(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusNotFound {
		t.Fatalf("error = %v, want 404 so the endpoint is not an enumeration oracle", err)
	}
}
