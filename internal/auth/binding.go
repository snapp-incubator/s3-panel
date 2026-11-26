package auth

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Cookie tossing.
//
// The session cookie is scoped to the panel's path, so it cannot carry the
// __Host- prefix, and any host under the same parent domain can set a cookie of
// the same name for the panel: Domain=<parent>, a longer Path such as
// /storage/api. The browser then sends it ahead of the panel's own, and the
// person works in the attacker's session (their uploads land in the attacker's
// buckets). Two defences:
//
//   - A request carrying the session cookie twice is refused, and every copy
//     the panel can name is cleared, so the person signs in again.
//   - With BindSession, the session is tied to a host-only companion cookie
//     that a sibling host cannot set, and the sealed session carries its value.
//     A session cookie that arrives without its own companion is refused.

// bindCookieSecure and bindCookieInsecure name the companion cookie. The
// __Host- prefix needs Secure, Path=/ and no Domain; a browser refuses it on
// any other terms, so with cookie_secure off (plain-HTTP development) the
// companion goes out unprefixed. It is still host-only there, and nothing on
// plain HTTP is safe from a sibling host anyway.
const (
	bindCookieSecure   = "__Host-s3panel_bind"
	bindCookieInsecure = "s3panel_bind"
)

// stateCookieBound names the sign-in state cookie when sessions are bound: a
// tossed state cookie would otherwise let a sibling host finish a sign-in in
// the person's browser with the attacker's own authorization code.
const stateCookieBound = "__Host-s3panel_oidc_state"

// maxTossedClears bounds the Set-Cookie headers one refusal sends.
const maxTossedClears = 48

func (a *Authenticator) bindCookieName() string {
	if a.cfg.CookieSecure {
		return bindCookieSecure
	}
	return bindCookieInsecure
}

// stateCookie is the name, and hostWide whether it is Path=/ host-only.
func (a *Authenticator) stateCookie() (name string, hostWide bool) {
	if a.BindSession && a.cfg.CookieSecure {
		return stateCookieBound, true
	}
	return stateCookieName, false
}

// setHostCookie sets a host-only cookie at Path=/, as __Host- requires.
func (a *Authenticator) setHostCookie(c echo.Context, name, value string, ttl time.Duration) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

func (a *Authenticator) clearHostCookie(c echo.Context, name string) {
	a.clearCookieAt(c, name, "/")
}

// requestCookies returns every value the request carries for name.
func requestCookies(r *http.Request, name string) []string {
	var out []string
	for _, ck := range r.Cookies() {
		if ck.Name == name {
			out = append(out, ck.Value)
		}
	}
	return out
}

// bindingHolds reports whether the request carries exactly one companion
// cookie and it matches the session's.
func (a *Authenticator) bindingHolds(r *http.Request, s Session) bool {
	if s.Binding == "" {
		return false
	}
	got := requestCookies(r, a.bindCookieName())
	if len(got) != 1 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got[0]), []byte(s.Binding)) == 1
}

// refuseTossed clears every copy of the session cookie this request could have
// carried, and the companion, so the next sign-in starts clean.
//
// A cookie the browser sent has a Path that prefixes this request's path and a
// Domain that is this host or a parent of it; clearing one takes the same
// name, Path and Domain, so each combination is cleared.
func (a *Authenticator) refuseTossed(c echo.Context) {
	req := c.Request()
	paths := dedupe(append([]string{a.cookiePath(), "/"}, pathAncestors(req.URL.Path)...))
	domains := append([]string{""}, parentDomains(req.Host)...)
	n := 0
	for _, d := range domains {
		for _, p := range paths {
			if n >= maxTossedClears {
				break
			}
			c.SetCookie(&http.Cookie{
				Name: a.cookieName(), Value: "", Path: p, Domain: d,
				HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
				Expires: time.Unix(0, 0), MaxAge: -1,
			})
			n++
		}
	}
	if a.BindSession {
		a.clearHostCookie(c, a.bindCookieName())
	}
}

// pathAncestors lists "/a", "/a/b", ... for "/a/b/c" (the path itself too).
func pathAncestors(p string) []string {
	var out []string
	for i := 1; i < len(p); i++ {
		if p[i] == '/' {
			out = append(out, p[:i])
		}
	}
	if p != "/" && p != "" {
		out = append(out, strings.TrimSuffix(p, "/"))
	}
	return out
}

// parentDomains lists the parents of host a cookie Domain could name, nearest
// first, stopping before the last label (no browser takes a TLD). None for an
// IP address or a single-label host.
func parentDomains(host string) []string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || net.ParseIP(host) != nil {
		return nil
	}
	labels := strings.Split(host, ".")
	var out []string
	for i := 0; i < len(labels)-1; i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}
