package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// TestReadOnlyOutranksPermissions is the point of the flag.
//
// A user may legitimately hold write or owner on a bucket, and in iam mode the
// panel holds a credential that can act on it — so a permission check alone
// would let the request through. When the instance is pointed at storage it must
// not change, the deployment's statement has to win.
func TestReadOnlyOutranksPermissions(t *testing.T) {
	reached := false
	handler := func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusOK)
	}

	s := &Server{Config: config.Config{Server: config.ServerConfig{ReadOnly: true}}}
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/object/upload?bucket=b", nil), rec)

	err := s.refuseWhenReadOnly()(handler)(c)

	if reached {
		t.Fatal("the handler ran; a read-only panel wrote to the storage backend")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusForbidden {
		t.Fatalf("error = %v, want 403", err)
	}
}

// TestReadOnlyOffIsATransparentPassthrough: the default deployment must be
// completely unaffected.
func TestReadOnlyOffIsATransparentPassthrough(t *testing.T) {
	reached := false
	handler := func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusOK)
	}

	s := &Server{Config: config.Config{Server: config.ServerConfig{ReadOnly: false}}}
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/object/upload?bucket=b", nil), httptest.NewRecorder())

	if err := s.refuseWhenReadOnly()(handler)(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Error("the handler did not run with read_only off")
	}
}

// TestConfigAdvertisesReadOnly: the SPA hides its controls from this, so it has
// to be reported rather than only enforced.
func TestConfigAdvertisesReadOnly(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		s := &Server{Config: config.Config{Server: config.ServerConfig{
			AuthMode: config.AuthModeIAM, ReadOnly: readOnly,
		}}}
		e := echo.New()
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/config", nil), rec)

		if err := s.HandleConfig()(c); err != nil {
			t.Fatalf("HandleConfig: %v", err)
		}
		want := `"read_only":` + map[bool]string{true: "true", false: "false"}[readOnly]
		if body := rec.Body.String(); !contains(body, want) {
			t.Errorf("config body %s, want %s", body, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// TestReadOnlyRefusesBeforeMinting is the ordering regression.
//
// Echo applies route middleware left to right, and with the credential listed
// first an upload or delete on a read-only panel performed a real STS mint of a
// READWRITE credential at the storage backend and only then returned 403. That
// contradicts the stated design and is a cost amplifier: a loop of deletes
// produces an unbounded stream of write-capable credentials to reap.
//
// Driven through gatewayChain rather than a hand-composed chain, so this fails
// if the routes' order is changed rather than only if this test's is.
func TestReadOnlyRefusesBeforeMinting(t *testing.T) {
	s, rec, done := credentialServer(t)
	defer done()
	s.Config.Server.ReadOnly = true

	reached := false
	handler := func(c echo.Context) error {
		reached = true
		return c.NoContent(http.StatusOK)
	}
	for _, m := range reverse(s.gatewayChain(permWrite, mutating)) {
		handler = m(handler)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/object/delete?bucket=okd4_teh_1__payments--invoices", nil)
	err := handler(withSession(e.NewContext(req, httptest.NewRecorder())))

	if reached {
		t.Fatal("the handler ran on a read-only panel")
	}
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusForbidden {
		t.Fatalf("error = %v, want 403", err)
	}
	if got := rec.seen(); len(got) != 0 {
		t.Errorf("minted %v before refusing; a read-only panel asked the backend for a write credential", got)
	}
}

// reverse composes a middleware slice the way echo does: the first element ends
// up outermost, so it is wrapped last.
func reverse(chain []echo.MiddlewareFunc) []echo.MiddlewareFunc {
	out := make([]echo.MiddlewareFunc, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		out = append(out, chain[i])
	}
	return out
}

// TestShareIsRefusedWhenReadOnly: the config comment on ReadOnly, the README and
// the PR description all say read-only refuses share links, and the route was
// the one mutating-by-policy route that did not carry the check.
func TestShareIsRefusedWhenReadOnly(t *testing.T) {
	s := &Server{Config: config.Config{Server: config.ServerConfig{ReadOnly: true}}}
	if len(s.gatewayChain(permRead, mutating)) != 3 {
		t.Fatal("a mutating route's chain must carry the read-only refusal")
	}
	if len(s.gatewayChain(permRead, reading)) != 2 {
		t.Error("a reading route must not carry the read-only refusal; browsing stays intact")
	}
}
