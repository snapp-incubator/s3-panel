package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// docsServer is the whole router, as main builds it, with the SPA on.
func docsServer(t *testing.T, mutate func(*config.Config)) *Server {
	t.Helper()
	s := &Server{Config: config.DefaultConfig(), logger: zap.NewNop()}
	mutate(&s.Config)
	s.registerRouter()
	s.registerRoutes()
	return s
}

var scriptTag = regexp.MustCompile(`<script[^>]*>`)

// TestDocsStandaloneRunUnderTheNoncePolicy: standalone, Swagger UI is served
// under an enforced nonce policy without Trusted Types, and every script tag of
// its page carries that response's nonce.
func TestDocsStandaloneRunUnderTheNoncePolicy(t *testing.T) {
	s := docsServer(t, func(*config.Config) {})

	rec := serve(s.Router, httptest.NewRequest(http.MethodGet, "/docs/index.html", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "SwaggerUIBundle") {
		t.Fatalf("GET /docs/index.html = %d, want Swagger UI:\n%s", rec.Code, body)
	}
	h := rec.Header()
	if ro := h.Values("Content-Security-Policy-Report-Only"); len(ro) != 0 {
		t.Errorf("a report-only policy is sent: %q", ro)
	}
	csp := h.Values("Content-Security-Policy")
	nonce := policyNonce(h)
	if len(csp) != 1 || nonce == "" || csp[0] != docsContentSecurityPolicy(nonce) {
		t.Fatalf("Content-Security-Policy = %q, want the docs policy", csp)
	}
	for _, want := range []string{"script-src 'nonce-" + nonce + "' 'strict-dynamic'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp[0], want) {
			t.Errorf("docs policy lacks %q: %q", want, csp[0])
		}
	}
	if strings.Contains(csp[0], "trusted-types") || strings.Contains(csp[0], "'unsafe-inline' 'self'") || strings.Contains(csp[0], "script-src 'self'") {
		t.Errorf("docs policy is not the page policy minus Trusted Types: %q", csp[0])
	}

	tags := scriptTag.FindAllString(body, -1)
	if len(tags) != 3 {
		t.Errorf("want the bundle, the preset and the initialiser, got %q", tags)
	}
	for _, tag := range tags {
		if strings.Count(tag, `nonce="`+nonce+`"`) != 1 {
			t.Errorf("script tag without the response nonce: %s", tag)
		}
	}
	if h.Get("Cache-Control") != "no-store" {
		t.Errorf("docs page is cacheable; a cached copy would replay its nonce")
	}

	// The bundle itself is served as is, under the docs policy.
	rec = serve(s.Router, httptest.NewRequest(http.MethodGet, "/docs/swagger-ui-bundle.js", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Header().Get("Content-Security-Policy"), "trusted-types") {
		t.Errorf("GET /docs/swagger-ui-bundle.js = %d, CSP %q", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}
}

// TestDocsOffInPanelMode: in a unified console there is no Swagger UI, and its
// paths are not answered by the SPA either.
func TestDocsOffInPanelMode(t *testing.T) {
	s := docsServer(t, func(c *config.Config) {
		c.Server.BasePath = "/storage"
		c.Panel = config.PanelConfig{Enabled: true}.WithDefaults()
	})

	for _, r := range s.Router.Routes() {
		if strings.Contains(r.Path, "/docs") {
			t.Errorf("docs route registered in panel mode: %s %s", r.Method, r.Path)
		}
	}
	for _, path := range []string{"/storage/docs/index.html", "/storage/docs/", "/storage/docs/doc.json", "/docs/index.html"} {
		rec := serve(s.Router, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusNotFound || strings.Contains(body, "<html") || strings.Contains(strings.ToLower(body), "swagger") {
			t.Errorf("GET %s = %d in panel mode, want a plain 404:\n%s", path, rec.Code, body)
		}
		if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "require-trusted-types-for 'script'") {
			t.Errorf("GET %s: 404 not under the page policy: %q", path, rec.Header().Get("Content-Security-Policy"))
		}
	}

	// The app itself is still served.
	if rec := serve(s.Router, httptest.NewRequest(http.MethodGet, "/storage/", nil)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("GET /storage/ = %d, want the SPA", rec.Code)
	}
}
