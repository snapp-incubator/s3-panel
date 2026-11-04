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
// handlers registered on the router.
var backendPrefixes = []string{"/api", "/s3/api", "/auth", "/health", "/livez", "/readyz", "/docs", "/iam/buckets"}

// frontendMiddleware serves the embedded frontend SPA for any GET or HEAD under
// the base path that is not an API, auth, health or docs route. A path naming a
// built file is served as that file; anything else falls back to index.html, so
// client-side routes (e.g. /object-storage/...) resolve to the app. Paths
// outside the base path are not the panel's and fall through.
//
// render answers for a page; serveIndex is the plain answer.
func frontendMiddleware(base string, render pageRenderer) echo.MiddlewareFunc {
	return spaMiddleware(web.FS(), base, render)
}

// pageRenderer answers a request for one of the SPA's pages, given index.html.
type pageRenderer func(c echo.Context, index []byte) error

// spaMiddleware is frontendMiddleware over any build, so tests can hand it one.
func spaMiddleware(files fs.FS, base string, render pageRenderer) echo.MiddlewareFunc {
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
			for _, prefix := range backendPrefixes {
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
