package api

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/web"
)

// cspNoncePlaceholder is what the frontend build writes wherever a nonce goes
// (vite.config.ts, html.cspNonce). Each response replaces it with that
// request's nonce, so the CSP can name exactly the scripts this page loads.
const cspNoncePlaceholder = "__CSP_NONCE__"

// backendPrefixes are the paths the SPA must never answer: they belong to
// handlers registered on the router. /docs stays here when Swagger UI is not
// registered (panel.enabled), so it answers 404 rather than the app.
var backendPrefixes = []string{"/api", "/s3/api", "/auth", "/health", "/livez", "/readyz", "/docs"}

// frontendMiddleware serves the embedded frontend SPA for any GET or HEAD under
// the base path that is not an API, auth, health or docs route. A path naming a
// built file is served as that file; anything else falls back to index.html, so
// client-side routes (e.g. /object-storage/...) resolve to the app. Paths
// outside the base path are not the panel's and fall through.
//
// render answers for a page; serveIndex is the plain answer.
//
// backend names further prefixes the SPA must leave to the router, for routes
// only some modes register.
func frontendMiddleware(base string, render pageRenderer, backend ...string) echo.MiddlewareFunc {
	return spaMiddleware(web.FS(), base, render, backend...)
}

// pageRenderer answers a request for one of the SPA's pages, given index.html.
type pageRenderer func(c echo.Context, index []byte) error

// spaMiddleware is frontendMiddleware over any build, so tests can hand it one.
func spaMiddleware(files fs.FS, base string, render pageRenderer, backend ...string) echo.MiddlewareFunc {
	skip := append(append([]string{}, backendPrefixes...), backend...)
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		// internal/web/dist always carries at least a placeholder index.html.
		panic("embedded frontend has no index.html: " + err.Error())
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if req.Method != http.MethodGet && req.Method != http.MethodHead {
				return next(c)
			}
			p, ok := underBase(req.URL.Path, base)
			if !ok {
				return next(c)
			}
			for _, prefix := range skip {
				if p == prefix || strings.HasPrefix(p, prefix+"/") {
					return next(c)
				}
			}

			name := strings.TrimPrefix(path.Clean("/"+p), "/")
			if name != "" && name != "index.html" {
				if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
					http.ServeFileFS(c.Response(), req, files, name)
					return nil
				}
			}
			return render(c, index)
		}
	}
}

// serveIndex answers with index.html, stamped with this request's CSP nonce.
//
// It is never cached: a cached copy would carry a nonce that no longer matches
// the policy it is served with, and reusing one nonce across responses is what
// a nonce exists to prevent.
func serveIndex(c echo.Context, index []byte) error {
	body := bytes.ReplaceAll(index, []byte(cspNoncePlaceholder), []byte(cspNonce(c)))
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.HTMLBlob(http.StatusOK, body)
}

// stampDocsNonce wraps Swagger UI (echoSwagger.WrapHandler) so it runs under
// an enforced nonce policy: every /docs/* answer carries
// docsContentSecurityPolicy, and index.html — whose script tags the library
// writes without a nonce — has this response's nonce stamped on each of them.
// The rest (the bundle, the stylesheet, doc.json) is served as is.
//
// index.html is never cached, for the same reason as the SPA's (serveIndex).
func stampDocsNonce(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		nonce := cspNonce(c)
		res := c.Response()
		setPolicy(res.Header(), docsContentSecurityPolicy(nonce), true)
		if path.Base(c.Request().URL.Path) != "index.html" {
			return next(c)
		}

		page := &capturedPage{ResponseWriter: res.Writer, status: http.StatusOK}
		res.Writer = page
		err := next(c)
		res.Writer = page.ResponseWriter
		if err != nil {
			return err
		}
		body := bytes.ReplaceAll(page.body.Bytes(), []byte("<script"), []byte(`<script nonce="`+nonce+`"`))
		res.Header().Del(echo.HeaderContentLength)
		res.Header().Set("Cache-Control", "no-store")
		page.ResponseWriter.WriteHeader(page.status)
		_, err = page.ResponseWriter.Write(body)
		return err
	}
}

// capturedPage holds a handler's answer back so it can be rewritten before it
// is sent. Headers still go straight to the real response.
type capturedPage struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (p *capturedPage) WriteHeader(code int) { p.status = code }

func (p *capturedPage) Write(b []byte) (int, error) { return p.body.Write(b) }

// underBase returns p relative to base ("/" for base itself), or false when p
// is not under base. With no base every path is the panel's.
func underBase(p, base string) (string, bool) {
	if base == "" {
		return p, true
	}
	if p == base {
		return "/", true
	}
	if rest, ok := strings.CutPrefix(p, base+"/"); ok {
		return "/" + rest, true
	}
	return "", false
}
