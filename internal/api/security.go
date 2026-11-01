package api

import (
	"crypto/rand"
	"encoding/base64"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// cspNonceKey is where securityHeaders leaves this request's CSP nonce, for the
// handler that renders index.html to stamp onto its script tags.
const cspNonceKey = "s3panel.csp_nonce"

// cspNonce returns the nonce securityHeaders generated for this request, or ""
// when it did not run.
func cspNonce(c echo.Context) string {
	v, _ := c.Get(cspNonceKey).(string)
	return v
}

// securityHeaders sets the response headers every page and API answer carries.
//
// The CSP is REPORT-ONLY for now: it names what the SPA should be loading, so a
// violation shows up in the browser console before anything is enforced. It is
// nonce-based rather than 'self'-based on purpose. When the panel shares an
// origin with other apps (a path per app on one host), 'self' would trust every
// app's script-typed responses, and the nonce is the only thing that says "this
// page put that script here".
//
// Framing is enforced, not reported: browsers ignore frame-ancestors in a
// report-only policy, and nothing frames this panel.
func securityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			nonce, err := newNonce()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not prepare the response")
			}
			c.Set(cspNonceKey, nonce)

			h := c.Response().Header()
			h.Set("Content-Security-Policy-Report-Only", contentSecurityPolicy(nonce))
			h.Set("Content-Security-Policy", "frame-ancestors 'none'")
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if isHTTPS(c.Request()) {
				// Sent only over HTTPS, where a browser honours it. No
				// includeSubDomains: the panel does not speak for other hosts.
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			return next(c)
		}
	}
}

func contentSecurityPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'nonce-" + nonce + "' 'strict-dynamic'",
		// The UI library sets inline styles at runtime.
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func newNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// isHTTPS reports whether the browser reached the panel over TLS, directly or
// through a TLS-terminating proxy.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// isSafeMethod reports whether a method only reads (RFC 9110 §9.2.1).
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// refuseCrossOrigin is the CSRF gate: a request that changes anything must come
// from the panel's own pages, or from no browser at all.
//
// SameSite=Lax does not do this on its own. It stops other SITES, not other
// hosts of the same site, and once the panel shares a host with other apps it
// stops nothing between them either — which is why the check is on Origin.
//
//   - No Origin: not a browser making a cross-origin request (curl, scripts,
//     another region's panel proxying a request it already checked). Accepted.
//   - Origin naming this request's own host, or a configured trusted origin:
//     accepted.
//   - "null" (sandboxed frames, some redirects, file: pages) and every other
//     origin: refused.
//
// Safe methods are never checked. The OIDC callback arrives through a
// cross-site redirect chain and must not be refused, and nothing a GET does
// changes state.
func (s *Server) refuseCrossOrigin() echo.MiddlewareFunc {
	trusted := map[string]bool{}
	// An origin an operator allowed for credentialed CORS can already read every
	// answer; letting it also write is no wider. The wildcard does not count.
	for _, raw := range append(append([]string{}, s.Config.Server.TrustedOrigins...), s.Config.Cors.AllowedOrigins...) {
		if o, ok := normalizeOrigin(raw); ok {
			trusted[o] = true
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if isSafeMethod(req.Method) {
				return next(c)
			}
			origin := req.Header.Get("Origin")
			if origin == "" {
				return next(c)
			}
			if o, ok := normalizeOrigin(origin); ok {
				if u, _ := url.Parse(o); u != nil && strings.EqualFold(u.Host, req.Host) {
					return next(c)
				}
				if trusted[o] {
					return next(c)
				}
			}
			return echo.NewHTTPError(http.StatusForbidden, "cross-origin request refused")
		}
	}
}

// normalizeOrigin reduces an origin to "scheme://host[:port]" in lower case,
// reporting false for anything that is not an http(s) origin — "null", "*",
// or a value with a path.
func normalizeOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" || raw == "null" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

// requireJSONBody refuses a body that is not JSON on any method that changes
// state, so an HTML form — which can only send form encodings or text/plain —
// cannot reach a JSON endpoint at all. It complements refuseCrossOrigin rather
// than relying on it.
//
// A request with no body passes: there is nothing to bind. Routes whose path
// ends in one of multipartSuffixes take multipart/form-data instead (uploads).
func requireJSONBody(multipartSuffixes ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			if isSafeMethod(req.Method) || req.ContentLength == 0 {
				return next(c)
			}
			mediaType, _, _ := mime.ParseMediaType(req.Header.Get(echo.HeaderContentType))
			if mediaType == echo.MIMEApplicationJSON {
				return next(c)
			}
			if mediaType == echo.MIMEMultipartForm {
				for _, suffix := range multipartSuffixes {
					if strings.HasSuffix(c.Path(), suffix) {
						return next(c)
					}
				}
			}
			return echo.NewHTTPError(http.StatusUnsupportedMediaType, "request body must be application/json")
		}
	}
}
