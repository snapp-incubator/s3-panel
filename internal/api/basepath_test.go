package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// newBasePathServer builds the real router for an s3-mode panel served at base.
func newBasePathServer(t *testing.T, base string) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Server.BasePath = base
	cfg.Server.ServeFrontend = false
	s := &Server{Config: cfg, logger: zap.NewNop()}
	s.registerRouter()
	s.registerRoutes()
	return s
}

// TestRoutesLiveUnderTheBasePath: with a base path the panel answers there and
// nowhere else, except /health, which an orchestrator probes at the root.
func TestRoutesLiveUnderTheBasePath(t *testing.T) {
	s := newBasePathServer(t, "/storage/")

	for path, want := range map[string]int{
		"/storage/api/config":      http.StatusOK,
		"/storage/s3/api/config":   http.StatusOK,
		"/storage/api/regions":     http.StatusOK,
		"/storage/health":          http.StatusOK,
		"/health":                  http.StatusOK,
		"/storage/livez":           http.StatusOK,
		"/storage/readyz":          http.StatusOK,
		"/livez":                   http.StatusOK,
		"/readyz":                  http.StatusOK,
		"/api/config":              http.StatusNotFound,
		"/s3/api/regions":          http.StatusNotFound,
		"/storagex/api/config":     http.StatusNotFound,
		"/storage/docs/index.html": http.StatusOK,
		"/docs/index.html":         http.StatusNotFound,
	} {
		if rec := serve(s.Router, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

// TestLoginURLCarriesTheBasePath: the SPA navigates to login_url as given, so
// it must already name the panel's own /auth/login, not the host's.
func TestLoginURLCarriesTheBasePath(t *testing.T) {
	for base, want := range map[string]string{"": "/auth/login", "/storage": "/storage/auth/login"} {
		s := &Server{Config: config.DefaultConfig()}
		s.Config.Server.AuthMode = config.AuthModeIAM
		s.Config.Server.BasePath = base
		e := echo.New()
		e.GET("/api/config", s.HandleConfig())
		rec := serve(e, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		var got ConfigResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.LoginURL != want {
			t.Errorf("base %q: login_url = %q, want %q", base, got.LoginURL, want)
		}
	}
}

// TestFrontendUnderTheBasePath: the SPA and its assets are served below the
// base; the host's other paths belong to other apps and fall through.
func TestFrontendUnderTheBasePath(t *testing.T) {
	e := echo.New()
	e.Use(securityHeaders())
	e.Use(spaMiddleware(builtFrontend, "/storage", serveIndex))
	e.GET("/storage/api/ping", func(c echo.Context) error { return c.String(http.StatusTeapot, "api") })

	for path, want := range map[string]int{
		"/storage":                   http.StatusOK,
		"/storage/":                  http.StatusOK,
		"/storage/object-storage/x":  http.StatusOK,
		"/storage/assets/app.js":     http.StatusOK,
		"/storage/api/ping":          http.StatusTeapot,
		"/":                          http.StatusNotFound,
		"/object-storage/x":          http.StatusNotFound,
		"/assets/app.js":             http.StatusNotFound,
		"/storagex/object-storage/x": http.StatusNotFound,
	} {
		if rec := serve(e, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestInvalidBasePathRefusesToStart(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.BasePath = "/a/../b"
	if _, err := NewServer(t.Context(), func() {}, cfg, zap.NewNop()); err == nil {
		t.Fatal("NewServer accepted a base path with ..")
	}
}

// TestHealthIsNotTheSPA: with the frontend served, livez and readyz must reach
// their handlers rather than fall back to index.html, or a prober would read
// the app's HTML as "healthy".
func TestHealthIsNotTheSPA(t *testing.T) {
	for _, base := range []string{"", "/storage"} {
		cfg := config.DefaultConfig()
		cfg.Server.BasePath = base
		s := &Server{Config: cfg, logger: zap.NewNop()}
		s.registerRouter()
		s.registerRoutes()

		rec := serve(s.Router, httptest.NewRequest(http.MethodGet, base+"/livez", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Errorf("base %q: livez = %d %q", base, rec.Code, rec.Body.String())
		}
		rec = serve(s.Router, httptest.NewRequest(http.MethodGet, base+"/readyz", nil))
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
			t.Errorf("base %q: readyz = %d %q", base, rec.Code, rec.Body.String())
		}
	}
}
