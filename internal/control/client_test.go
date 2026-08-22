package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const listBucketsXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>u-alice</ID><DisplayName>Alice</DisplayName></Owner>
  <Buckets>
    <Bucket>
      <Name>okd4_teh_1__payments--exports</Name>
      <CreationDate>2026-01-01T00:00:00Z</CreationDate>
      <BucketRegion>teh-1</BucketRegion>
      <Tenant>okd4_teh_1__payments</Tenant>
      <RealName>exports</RealName>
      <S3Name>okd4_teh_1__payments:exports</S3Name>
      <Permissions><Permission>read</Permission><Permission>write</Permission></Permissions>
      <GrantedViaTeam>payments</GrantedViaTeam>
    </Bucket>
    <Bucket>
      <Name>okd4_teh_2__payments--exports</Name>
      <CreationDate>2026-01-01T00:00:00Z</CreationDate>
      <BucketRegion>teh-2</BucketRegion>
      <RealName>exports</RealName>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

// TestListBucketsSpansRegions is the property the whole mode rests on: one call
// returns buckets from every region, so the panel never has to fan out.
func TestListBucketsSpansRegions(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(listBucketsXML))
	}))
	defer srv.Close()

	c, err := New(srv.URL, "", time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	buckets, err := c.ListBuckets(context.Background(), "user-token")
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}

	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2", len(buckets))
	}
	if buckets[0].BucketRegion == buckets[1].BucketRegion {
		t.Error("both buckets landed in the same region; the listing is not cross-region")
	}
	// The panel forwards the user's own token, so the endpoint answers for the
	// person at the keyboard rather than for an ambient service credential.
	if gotAuth != "Bearer user-token" {
		t.Errorf("Authorization = %q, want the caller's bearer token", gotAuth)
	}
}

func TestBucketFields(t *testing.T) {
	tenanted := Bucket{
		Name: "okd4_teh_1__payments--exports", RealName: "exports",
		S3Name: "okd4_teh_1__payments:exports", Permissions: []string{"read", "write"},
	}
	if got := tenanted.DisplayName(); got != "exports" {
		t.Errorf("DisplayName = %q, want the bare bucket name", got)
	}
	if !tenanted.Can("read") || !tenanted.Can("WRITE") {
		t.Error("Can should match held permissions, case-insensitively")
	}
	if tenanted.Can("owner") {
		t.Error("Can returned true for a permission that is not held")
	}

	// A plain S3 endpoint reports no permissions; it has already decided access
	// by answering the call at all, so the panel must not hide everything.
	plain := Bucket{Name: "data"}
	if !plain.Can("owner") {
		t.Error("a bucket with no reported permissions should not be treated as forbidden")
	}
	if got := plain.DisplayName(); got != "data" {
		t.Errorf("DisplayName = %q", got)
	}
}

// TestErrorCarriesStatus: an authorization failure has to stay recognisable, or
// the panel reports "backend unavailable" when the real answer is "you may not".
func TestErrorCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>nope</Message></Error>`))
	}))
	defer srv.Close()

	c, _ := New(srv.URL, "", time.Second)
	_, err := c.ListBuckets(context.Background(), "t")
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *Error", err)
	}
	if apiErr.Status != http.StatusForbidden || apiErr.Code != "AccessDenied" || apiErr.Message != "nope" {
		t.Errorf("error = %+v", apiErr)
	}
}

// TestBucketPolicyAbsentIsNotAnError: a bucket with no policy is an ordinary
// state and must not surface as a failure on the detail page.
func TestBucketPolicyAbsentIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchBucketPolicy</Code><Message>none</Message></Error>`))
	}))
	defer srv.Close()

	c, _ := New(srv.URL, "", time.Second)
	policy, err := c.BucketPolicy(context.Background(), "t", "b", "teh-1")
	if err != nil {
		t.Fatalf("BucketPolicy: %v", err)
	}
	if policy != nil {
		t.Errorf("policy = %s, want nil", policy)
	}
}

func TestSessionCredentials(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotForm = string(body)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<GetSessionTokenResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetSessionTokenResult>
    <Credentials>
      <AccessKeyId>AK</AccessKeyId><SecretAccessKey>SK</SecretAccessKey>
      <SessionToken></SessionToken><Expiration>2026-08-11T03:00:00Z</Expiration>
    </Credentials>
    <Endpoint>https://rgw.example</Endpoint><Region>teh-1</Region><SessionId>sess-1</SessionId>
  </GetSessionTokenResult>
</GetSessionTokenResponse>`))
	}))
	defer srv.Close()

	c, _ := New("http://unused/s3", srv.URL, time.Second)
	cred, err := c.SessionCredentials(context.Background(), "token", "teh-1", "okd4_teh_1__payments", time.Hour)
	if err != nil {
		t.Fatalf("SessionCredentials: %v", err)
	}
	if cred.AccessKeyID != "AK" || cred.SecretAccessKey != "SK" {
		t.Errorf("credentials = %+v", cred)
	}
	// The endpoint travels with the credential: object calls go there directly,
	// not back through the control endpoint.
	if cred.Endpoint != "https://rgw.example" {
		t.Errorf("endpoint = %q", cred.Endpoint)
	}
	if !strings.Contains(gotForm, "Action=GetSessionToken") || !strings.Contains(gotForm, "Region=teh-1") {
		t.Errorf("form = %q", gotForm)
	}
}

func TestDeriveSTSURL(t *testing.T) {
	cases := map[string]string{
		"https://iam.example.com/s3":  "https://iam.example.com/sts",
		"http://localhost:8082/s3":    "http://localhost:8082/sts",
		"https://iam.example.com/api": "https://iam.example.com/sts",
	}
	for in, want := range cases {
		if got := deriveSTSURL(in); got != want {
			t.Errorf("deriveSTSURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewRequiresControlURL(t *testing.T) {
	if _, err := New("", "", time.Second); err == nil {
		t.Error("New accepted an empty control_url")
	}
}

// TestSessionCredentialsSendsTheTenant pins that a pinned tenant actually
// reaches the control endpoint.
//
// One credential is a subuser under a single storage account and so reaches
// exactly one tenant's buckets. If this parameter is dropped the endpoint picks
// a tenant on the caller's behalf and every bucket outside it answers "access
// denied" — the failure looks like broken storage, not a missing form field.
func TestSessionCredentialsSendsTheTenant(t *testing.T) {
	var gotTenant, gotRegion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotTenant = r.FormValue("Tenant")
		gotRegion = r.FormValue("Region")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<GetSessionTokenResponse><GetSessionTokenResult><Credentials>
  <AccessKeyId>AK</AccessKeyId><SecretAccessKey>SK</SecretAccessKey>
</Credentials></GetSessionTokenResult></GetSessionTokenResponse>`))
	}))
	defer srv.Close()

	c, _ := New("http://unused/s3", srv.URL, time.Second)
	if _, err := c.SessionCredentials(context.Background(), "token", "teh-1", "okd4_teh_1__analytics", time.Hour); err != nil {
		t.Fatalf("SessionCredentials: %v", err)
	}
	if gotTenant != "okd4_teh_1__analytics" {
		t.Errorf("Tenant = %q, want okd4_teh_1__analytics", gotTenant)
	}
	if gotRegion != "teh-1" {
		t.Errorf("Region = %q, want teh-1", gotRegion)
	}
}

// TestSessionCredentialsOmitsAnEmptyTenant keeps the parameter optional, so a
// single-tenant caller behaves exactly as before.
func TestSessionCredentialsOmitsAnEmptyTenant(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, present = r.Form["Tenant"]
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<GetSessionTokenResponse><GetSessionTokenResult><Credentials>
  <AccessKeyId>AK</AccessKeyId><SecretAccessKey>SK</SecretAccessKey>
</Credentials></GetSessionTokenResult></GetSessionTokenResponse>`))
	}))
	defer srv.Close()

	c, _ := New("http://unused/s3", srv.URL, time.Second)
	if _, err := c.SessionCredentials(context.Background(), "token", "teh-1", "", time.Hour); err != nil {
		t.Fatalf("SessionCredentials: %v", err)
	}
	if present {
		t.Error("an empty tenant was sent as a form field; it must be omitted")
	}
}
