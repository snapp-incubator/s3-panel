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
