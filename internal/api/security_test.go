package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// builtFrontend stands in for a Vite build: index.html carries the nonce
// placeholder on its script tag, as html.cspNonce writes it.
var builtFrontend = fstest.MapFS{
	"index.html": {Data: []byte(`<!doctype html><html><head>` +
		`<script type="module" src="/assets/app.js" nonce="` + cspNoncePlaceholder + `"></script>` +
		`</head><body><div id="root"></div></body></html>`)},
	"assets/app.js": {Data: []byte("console.log('app')")},
}

func newSecuredRouter(s *Server) *echo.Echo {
	e := echo.New()
	e.Use(securityHeaders())
	e.Use(s.refuseCrossOrigin())
	e.Use(spaMiddleware(builtFrontend, "", serveIndex))
	ok := func(c echo.Context) error { return c.NoContent(http.StatusNoContent) }
	e.POST("/api/thing", ok, requireJSONBody("/object/upload"))
	e.POST("/api/object/upload", ok, requireJSONBody("/object/upload"))
	e.GET("/api/thing", func(c echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"ok": "yes"}) })
	return e
}

func serve(e *echo.Echo, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestSecurityHeadersOnEveryAnswer: the SPA, an API answer and an error all
// carry the same headers, because a header only on the happy path is a header
// an attacker routes around.
func TestSecurityHeadersOnEveryAnswer(t *testing.T) {
	e := newSecuredRouter(&Server{})
	for _, path := range []string{"/", "/object-storage/s3-bucket", "/api/thing", "/api/missing"} {
		rec := serve(e, httptest.NewRequest(http.MethodGet, path, nil))
		h := rec.Header()
		for name, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "strict-origin-when-cross-origin",
			"X-Frame-Options":         "DENY",
			"Content-Security-Policy": "frame-ancestors 'none'",
		} {
			if got := h.Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", path, name, got, want)
			}
		}
		csp := h.Get("Content-Security-Policy-Report-Only")
		if !strings.Contains(csp, "script-src 'nonce-") || !strings.Contains(csp, "'strict-dynamic'") {
			t.Errorf("%s: report-only CSP is not nonce-based: %q", path, csp)
		}
		if strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: script-src trusts 'self', which on a shared origin trusts every app", path)
		}
		if h.Get("Strict-Transport-Security") != "" {
			t.Errorf("%s: HSTS sent over plain HTTP", path)
		}
	}
}

func TestHSTSOnlyOverHTTPS(t *testing.T) {
	e := newSecuredRouter(&Server{})
	req := httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := serve(e, req).Header().Get("Strict-Transport-Security"); !strings.HasPrefix(got, "max-age=") {
		t.Errorf("HSTS = %q behind a TLS-terminating proxy, want max-age", got)
	}
}

// TestIndexCarriesTheResponseNonce: the nonce in the page and the one in the
// policy must be the same value, fresh per response, or the policy either
// blocks the app or protects nothing.
func TestIndexCarriesTheResponseNonce(t *testing.T) {
	e := newSecuredRouter(&Server{})

	nonces := map[string]bool{}
	for range 2 {
		rec := serve(e, httptest.NewRequest(http.MethodGet, "/object-storage/s3-bucket", nil))
		body := rec.Body.String()
		if strings.Contains(body, cspNoncePlaceholder) {
			t.Fatalf("placeholder left in the page: %s", body)
		}
		csp := rec.Header().Get("Content-Security-Policy-Report-Only")
		_, after, _ := strings.Cut(csp, "'nonce-")
		nonce, _, _ := strings.Cut(after, "'")
		if nonce == "" || !strings.Contains(body, `nonce="`+nonce+`"`) {
			t.Fatalf("page does not carry the policy's nonce %q: %s", nonce, body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("index.html is cacheable; a cached copy would replay its nonce")
		}
		nonces[nonce] = true
	}
	if len(nonces) != 2 {
		t.Errorf("two responses shared a nonce")
	}

	// A built asset is served as itself, not as the page.
	rec := serve(e, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("asset: %d %q", rec.Code, rec.Body.String())
	}
}

func TestCrossOriginWritesAreRefused(t *testing.T) {
	s := &Server{Config: config.Config{
		Server: config.ServerConfig{TrustedOrigins: []string{"http://localhost:5173"}},
		// The CORS wildcard must not count as trusting every origin.
		Cors: config.ServerCorsConfig{AllowedOrigins: []string{"*", "https://console.example.com"}},
	}}
	e := newSecuredRouter(s)

	cases := []struct {
		name, method, origin string
		want                 int
	}{
		{"no origin (a script)", http.MethodPost, "", http.StatusNoContent},
		{"own origin", http.MethodPost, "https://panel.example.com", http.StatusNoContent},
		{"own origin, any case", http.MethodPost, "https://PANEL.example.com", http.StatusNoContent},
		{"trusted dev origin", http.MethodPost, "http://localhost:5173", http.StatusNoContent},
		{"explicit CORS origin", http.MethodPost, "https://console.example.com", http.StatusNoContent},
		{"another host on the same site", http.MethodPost, "https://evil.example.com", http.StatusForbidden},
		{"another port", http.MethodPost, "https://panel.example.com:8443", http.StatusForbidden},
		{"null", http.MethodPost, "null", http.StatusForbidden},
		{"garbage", http.MethodPost, "not a url", http.StatusForbidden},
		{"a read is never checked", http.MethodGet, "https://evil.example.com", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "https://panel.example.com/api/thing", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if rec := serve(e, req); rec.Code != tc.want {
				t.Errorf("%s %s from %q = %d, want %d", tc.method, req.URL.Path, tc.origin, rec.Code, tc.want)
			}
		})
	}
}

// TestFormBodiesCannotReachJSONRoutes: an HTML form can send only form
// encodings and text/plain, so refusing those keeps every JSON route out of a
// form's reach.
func TestFormBodiesCannotReachJSONRoutes(t *testing.T) {
	e := newSecuredRouter(&Server{})
	cases := []struct {
		name, path, contentType, body string
		want                          int
	}{
		{"json", "/api/thing", "application/json", `{"email":"x"}`, http.StatusNoContent},
		{"json with charset", "/api/thing", "application/json; charset=utf-8", `{}`, http.StatusNoContent},
		{"no body", "/api/thing", "", "", http.StatusNoContent},
		{"urlencoded form", "/api/thing", "application/x-www-form-urlencoded", "email=x", http.StatusUnsupportedMediaType},
		{"text/plain form", "/api/thing", "text/plain", `{"email":"x"}`, http.StatusUnsupportedMediaType},
		{"multipart elsewhere", "/api/thing", "multipart/form-data; boundary=x", "--x--", http.StatusUnsupportedMediaType},
		{"multipart upload", "/api/object/upload", "multipart/form-data; boundary=x", "--x--", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if rec := serve(e, req); rec.Code != tc.want {
				t.Errorf("POST %s (%s) = %d, want %d", tc.path, tc.contentType, rec.Code, tc.want)
			}
		})
	}
}

// TestShareLinksArePOST: minting a share link is an action, so the GET that
// any image tag could trigger is gone from both API prefixes.
func TestShareLinksArePOST(t *testing.T) {
	s := &Server{Config: config.DefaultConfig(), logger: zap.NewNop()}
	s.Config.Server.ServeFrontend = false
	s.registerRouter()
	s.registerRoutes()

	for _, prefix := range []string{"/api", "/s3/api"} {
		// The group's not-found route answers 404 rather than 405.
		rec := serve(s.Router, httptest.NewRequest(http.MethodGet, prefix+"/object/share?bucket=b&object=o", nil))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s/object/share = %d, want it unrouted", prefix, rec.Code)
		}
		found := false
		for _, r := range s.Router.Routes() {
			if r.Path == prefix+"/object/share" && r.Method == http.MethodPost {
				found = true
			}
			if r.Path == prefix+"/object/share" && r.Method == http.MethodGet {
				t.Errorf("GET %s/object/share is still routed", prefix)
			}
		}
		if !found {
			t.Errorf("no POST %s/object/share route", prefix)
		}
	}
}
