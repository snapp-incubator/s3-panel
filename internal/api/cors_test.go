package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// TestCORSDefaultsToNoOrigins: the binary's default answers no cross-origin
// request with CORS headers, wildcard included; a configured origin gets them.
func TestCORSDefaultsToNoOrigins(t *testing.T) {
	probe := func(cfg config.Config, origin string) http.Header {
		s := &Server{Config: cfg, logger: zap.NewNop()}
		e := echo.New()
		e.GET("/api/x", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, s.CORSMiddleware())
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Header()
	}

	def := config.DefaultConfig()
	if len(def.Cors.AllowedOrigins) != 0 {
		t.Fatalf("default allowed origins = %q, want none", def.Cors.AllowedOrigins)
	}
	h := probe(def, "https://evil.example")
	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("default: Access-Control-Allow-Origin = %q, want none", got)
	}
	if got := h.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("default: Access-Control-Allow-Credentials = %q, want none", got)
	}

	cfg := config.DefaultConfig()
	cfg.Cors.AllowedOrigins = []string{"https://console.example.com"}
	if got := probe(cfg, "https://console.example.com").Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Errorf("configured: Access-Control-Allow-Origin = %q", got)
	}
	if got := probe(cfg, "https://evil.example").Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("configured, other origin: Access-Control-Allow-Origin = %q, want none", got)
	}
}
