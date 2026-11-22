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
// The CSP is ENFORCED, in every mode. It is nonce-based rather than
// 'self'-based on purpose. When the panel shares an origin with other apps (a
// path per app on one host), 'self' would trust every app's script-typed
// responses, and the nonce is the only thing that says "this page put that
// script here".
//
// It also requires Trusted Types for every DOM sink that can run script, as the
// console's own pages do (panel-shell). The SPA bundle writes no markup from
// strings except one constant stylesheet in a dependency, which the bundle's
// own "default" policy admits (frontend/src/lib/trustedTypes.ts); no other
// policy may be created.
//
// Swagger UI (<base>/docs, standalone only) cannot run under Trusted Types and
// gets its own policy: see docsContentSecurityPolicy.
func securityHeaders() echo.MiddlewareFunc {
	return securityHeadersEnforcing(true)
}

// setCSP writes the policy as enforced or report-only. Enforced, the policy
// already carries frame-ancestors, so it is the one Content-Security-Policy
// header; report-only, frame-ancestors also goes out as its own enforced
// header, because browsers ignore it in a report-only policy. One function
// for both, so switching modes cannot leave a second h.Set on the same header
// silently replacing the policy.
//
// Report-only is kept for securityHeadersEnforcing(false), a switch for
// trying a stricter policy out without breaking anything; nothing in the
// server uses it today.
func setCSP(h http.Header, nonce string, enforce bool) {
	setPolicy(h, contentSecurityPolicy(nonce), enforce)
}

func setPolicy(h http.Header, policy string, enforce bool) {
	if enforce {
		h.Del("Content-Security-Policy-Report-Only")
		h.Set("Content-Security-Policy", policy)
		return
	}
	h.Set("Content-Security-Policy-Report-Only", policy)
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
}

func securityHeadersEnforcing(enforce bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			nonce, err := newNonce()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "could not prepare the response")
			}
			c.Set(cspNonceKey, nonce)

			h := c.Response().Header()
			setCSP(h, nonce, enforce)
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

// baseDirectives is what every page's policy holds: scripts only by this
// response's nonce (and what those scripts load), nothing framing the page.
func baseDirectives(nonce string) []string {
	return []string{
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
	}
}

// contentSecurityPolicy is the policy of the SPA, the No access page and every
// API answer.
//
// require-trusted-types-for makes every string written to a script-running
// sink (innerHTML, script.src, eval, …) pass a Trusted Types policy first.
// trusted-types names the one policy that may exist: the SPA's "default",
// which admits markup-free strings only (frontend/src/lib/trustedTypes.ts).
// Neither the SPA nor the console header creates any other; a script that
// tries is refused, which is the point. 'none' would forbid the default policy
// too, and a dependency (Radix Select) writes a constant stylesheet through
// innerHTML that only a default policy can admit.
func contentSecurityPolicy(nonce string) string {
	return strings.Join(append(baseDirectives(nonce),
		"require-trusted-types-for 'script'",
		"trusted-types default",
	), "; ")
}

// docsContentSecurityPolicy is the policy of Swagger UI (<base>/docs/*, served
// only standalone). It is the page policy minus Trusted Types, enforced:
//
//   - swagger-ui-bundle.js writes markup through innerHTML in many places
//     (React and DOMPurify among others) and ships no Trusted Types policy of
//     its own, so it cannot run with require-trusted-types-for. Admitting it
//     would take a permissive default policy, which would then cover the SPA
//     too; per-path policies keep the SPA's strict.
//   - Its scripts are still admitted only by nonce: swaggerUI stamps this
//     response's nonce on the tags of its index.html, so the page keeps the
//     same script-src as the SPA and no 'self' or 'unsafe-inline'.
//
// Not report-only: a report-only policy would leave /docs protected by
// frame-ancestors alone, and the nonce policy is what keeps an injected
// script off the page.
func docsContentSecurityPolicy(nonce string) string {
	return strings.Join(baseDirectives(nonce), "; ")
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
