package api

import (
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

// writableBucketXML is a bucket the signed-in user may both read and write, so
// the same bucket can be exercised from a read route and a write route.
const writableBucketXML = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>u-alice</ID><DisplayName>Alice</DisplayName></Owner>
  <Buckets>
    <Bucket>
      <Name>okd4_teh_1__payments--invoices</Name>
      <BucketRegion>teh-1</BucketRegion>
      <Tenant>okd4_teh_1__payments</Tenant>
      <RealName>invoices</RealName>
      <S3Name>okd4_teh_1__payments:invoices</S3Name>
      <Permissions><Permission>read</Permission><Permission>write</Permission></Permissions>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>`

// mintRecorder is a control endpoint that answers bucket listings and STS, and
// remembers every access level it was asked for.
type mintRecorder struct {
	mu   sync.Mutex
	asks []string
}

func (m *mintRecorder) record(level string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asks = append(m.asks, level)
}

func (m *mintRecorder) seen() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.asks...)
}

// credentialServer wires an IAM-mode Server to a control endpoint that records
// what the panel asked for. Each mint returns a distinct key so a cache hit is
// distinguishable from a fresh mint.
func credentialServer(t *testing.T) (*Server, *mintRecorder, func()) {
	t.Helper()
	rec := &mintRecorder{}
	var n int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			rec.record(r.FormValue("Access"))
			n++
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<GetSessionTokenResponse><GetSessionTokenResult><Credentials>
  <AccessKeyId>AK` + strings.Repeat("x", n) + `</AccessKeyId>
  <SecretAccessKey>SK</SecretAccessKey>
</Credentials></GetSessionTokenResult></GetSessionTokenResponse>`))
			return
		}
		_, _ = w.Write([]byte(writableBucketXML))
	}))

	client, err := control.New(srv.URL, srv.URL, time.Second)
	if err != nil {
		t.Fatalf("control.New: %v", err)
	}
	s := &Server{
		Config:      config.Config{Server: config.ServerConfig{AuthMode: config.AuthModeIAM, Region: "teh-1"}},
		control:     client,
		logger:      zap.NewNop(),
		credentials: newCredentialCache(),
	}
	return s, rec, srv.Close
}

// drive runs the real middleware chain for one route: the permission gate, then
// credential injection, in the order the router composes them.
func drive(t *testing.T, s *Server, method, permission string) string {
	t.Helper()
	var injected string
	handler := func(c echo.Context) error {
		injected = c.Request().Header.Get(accessKeyHeader)
		return c.NoContent(http.StatusOK)
	}
	e := echo.New()
	req := httptest.NewRequest(method, "/object/x?bucket=okd4_teh_1__payments--invoices", nil)
	c := withSession(e.NewContext(req, httptest.NewRecorder()))

	chain := s.requireBucketPermission(permission)(s.injectObjectCredentials()(handler))
	if err := chain(c); err != nil {
		t.Fatalf("%s %s: unexpected error: %v", method, permission, err)
	}
	return injected
}

// TestCredentialLevelFollowsTheRoute is the point of the whole mechanism.
//
// The level asked for must come from the ROUTE, not from what the user could do.
// A listing of a bucket the user can write must still be signed with a credential
// that cannot write — otherwise the permission gate is the only thing between a
// bug and a mutation.
func TestCredentialLevelFollowsTheRoute(t *testing.T) {
	s, rec, done := credentialServer(t)
	defer done()

	drive(t, s, http.MethodGet, permRead)
	if got := rec.seen(); len(got) != 1 || got[0] != accessRead {
		t.Fatalf("read route asked for %v, want [read] — the user holds write, but this request does not need it", got)
	}

	drive(t, s, http.MethodPost, permWrite)
	if got := rec.seen(); len(got) != 2 || got[1] != accessReadWrite {
		t.Fatalf("write route asked for %v, want the second to be readwrite", got)
	}
}

// TestCredentialCacheIsKeyedByLevel is the trap that would turn this change from
// defence in depth into broken uploads: with the level missing from the key, the
// listing's read-only credential is served to the next upload and the gateway
// refuses it.
func TestCredentialCacheIsKeyedByLevel(t *testing.T) {
	s, rec, done := credentialServer(t)
	defer done()

	readKey := drive(t, s, http.MethodGet, permRead)
	writeKey := drive(t, s, http.MethodPost, permWrite)

	if readKey == writeKey {
		t.Fatal("the upload was handed the listing's credential; the cache key is missing the access level")
	}
	if got := rec.seen(); len(got) != 2 {
		t.Fatalf("%d mints for two different levels, want 2: %v", len(got), got)
	}

	// The cache must still work WITHIN a level, or every page view mints again.
	if again := drive(t, s, http.MethodGet, permRead); again != readKey {
		t.Errorf("second read minted %q instead of reusing %q", again, readKey)
	}
	if got := rec.seen(); len(got) != 2 {
		t.Errorf("the repeat read minted again (%v); the cache is not serving hits", got)
	}
}

// TestRequiredAccessDefaultsToRead: a route that reaches credential injection
// without declaring a permission must not obtain a credential that can mutate.
func TestRequiredAccessDefaultsToRead(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())

	if got := requiredAccess(c); got != accessRead {
		t.Errorf("an unset permission yielded %q, want read", got)
	}
	for perm, want := range map[string]string{
		permRead:  accessRead,
		permWrite: accessReadWrite,
		permOwner: accessReadWrite,
	} {
		c.Set(requiredPermissionKey, perm)
		if got := requiredAccess(c); got != want {
			t.Errorf("permission %q yielded %q, want %q", perm, got, want)
		}
	}
}
