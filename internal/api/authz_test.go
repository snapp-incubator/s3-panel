package api

import (
	"bytes"
	"mime/multipart"
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

// TestMultipartUploadBucketIsRewritten covers the path that broke uploads while
// listing kept working.
//
// An upload is multipart/form-data, and nothing has parsed it when the
// authorization middleware runs. The rewrite used to skip an unparsed form to
// avoid consuming the body, which meant it never fired for uploads at all: the
// handler parsed the body itself and bound the CONTROL-PLANE bucket name, which
// the gateway has never heard of. The user saw a 422 "no such bucket" on upload
// for a bucket they could browse perfectly well.
func TestMultipartUploadBucketIsRewritten(t *testing.T) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("bucket", "okd4_teh_1__payments--exports"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	fw, err := w.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte("payload")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/objects/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	c := echo.New().NewContext(req, httptest.NewRecorder())

	rewriteBucketParam(c, "okd4_teh_1__payments:exports")

	// This is exactly how the handler reads it.
	if got := c.FormValue("bucket"); got != "okd4_teh_1__payments:exports" {
		t.Fatalf("handler would upload to %q, which the gateway does not know", got)
	}

	// And the file must survive the early parse, or the upload has nothing to send.
	form, err := c.MultipartForm()
	if err != nil {
		t.Fatalf("multipart form after rewrite: %v", err)
	}
	if len(form.File["files"]) != 1 {
		t.Fatalf("got %d files after the rewrite parsed the body; the upload payload was lost",
			len(form.File["files"]))
	}
}

// multipartUpload builds an upload request shaped exactly like the panel's own:
// the bucket travels as a form field, never in the query string.
func multipartUpload(t *testing.T, bucket string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("bucket", bucket); err != nil {
		t.Fatalf("write field: %v", err)
	}
	fw, err := w.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte("payload")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/object/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// TestUploadIsPermissionChecked is the security half of the upload bug.
//
// The middleware read the bucket only from the query string. An upload carries
// it in a multipart field, so the lookup returned "" and the middleware took its
// "this route names no bucket, nothing to check" path — handing the upload
// straight to the handler UNCHECKED. Any signed-in user could write to any
// bucket the panel's credential could reach, including ones they hold no write
// on and ones they were never granted at all.
//
// The stub grants read only, so an upload must be refused.
func TestUploadIsPermissionChecked(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	reached := false
	handler := func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusOK)
	}

	req := multipartUpload(t, "okd4_teh_1__payments--exports")
	rec := httptest.NewRecorder()
	c := withSession(echo.New().NewContext(req, rec))

	err := s.requireBucketPermission(permWrite)(handler)(c)

	if reached {
		t.Fatal("upload reached the handler without a write check — any user could write to any bucket")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusForbidden {
		t.Fatalf("got %v, want 403 — the caller holds read, not write", err)
	}
}

// TestUploadToAnUngrantedBucketIsRefused checks the same path for a bucket the
// caller was never granted at all. It must read as absent rather than forbidden,
// so the endpoint cannot be used to enumerate buckets.
func TestUploadToAnUngrantedBucketIsRefused(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	reached := false
	handler := func(c echo.Context) error { reached = true; return c.NoContent(http.StatusOK) }

	req := multipartUpload(t, "someone-elses-bucket")
	c := withSession(echo.New().NewContext(req, httptest.NewRecorder()))

	err := s.requireBucketPermission(permWrite)(handler)(c)

	if reached {
		t.Fatal("upload to an ungranted bucket reached the handler")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Fatalf("got %v, want 404", err)
	}
}

// TestResolvedBucketIsPublishedForLaterMiddleware guards the contract the
// per-tenant credential depends on.
//
// The credential middleware picks WHICH tenant to mint for by reading the bucket
// this middleware resolved. If that stops being published — or if the credential
// middleware is hoisted back onto the route group, where echo runs it before
// this one — the tenant silently reads as empty, every user falls back to a
// single credential per session, and everyone whose buckets span tenants gets
// "access denied" on all but one of them.
func TestResolvedBucketIsPublishedForLaterMiddleware(t *testing.T) {
	s, done := iamModeServer(t)
	defer done()

	var tenant string
	var found bool
	handler := func(c echo.Context) error {
		b, ok := resolvedBucket(c)
		found = ok
		tenant = b.Tenant
		return c.NoContent(http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/object/list?bucket=okd4_teh_1__payments--exports", nil)
	c := withSession(echo.New().NewContext(req, httptest.NewRecorder()))

	if err := s.requireBucketPermission(permRead)(handler)(c); err != nil {
		t.Fatalf("requireBucketPermission: %v", err)
	}
	if !found {
		t.Fatal("no resolved bucket was published; the credential middleware cannot know the tenant")
	}
	if tenant != "okd4_teh_1__payments" {
		t.Fatalf("tenant = %q, want okd4_teh_1__payments", tenant)
	}
}
