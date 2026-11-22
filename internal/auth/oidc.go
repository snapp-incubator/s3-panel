package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// stateCookieName holds the CSRF state and nonce between /auth/login and
// /auth/callback. It is short-lived and cleared as soon as the callback runs.
const stateCookieName = "s3panel_oidc_state"

// stateTTL bounds how long a login may sit unfinished.
const stateTTL = 10 * time.Minute

// signedOutCookieName marks a browser that just signed out explicitly, until
// the next /auth/me reports it and clears it (see MarkSignOut).
const signedOutCookieName = "s3panel_signed_out"

// signedOutTTL bounds how long the sign-out marker waits for the next visit.
const signedOutTTL = 10 * time.Minute

// DefaultSessionTTL is how long a sign-in lasts when unconfigured. It bounds the
// session independently of the access token, which is refreshed underneath it.
const DefaultSessionTTL = 12 * time.Hour

// Authenticator runs the OIDC flow and owns the session cookie.
type Authenticator struct {
	cfg      config.OIDCConfig
	provider *oidc.Provider
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	codec    *CookieCodec

	// OnLogout runs before the session cookie is cleared, with the session that
	// is about to end. The server uses it to revoke the storage credentials this
	// console obtained for the person; left nil, logout just clears the cookie.
	OnLogout func(context.Context, Session)

	// MarkSignOut has Logout leave a marker that the next /auth/me reports once
	// ("signed_out": true) and clears. Part of a console, the SPA signs in
	// silently through the provider, at most once per short window so a sign-in
	// that does not stick cannot loop; an explicit sign-out followed by a quick
	// return is not such a loop, and the marker says so. Off, Logout sets no
	// marker and nothing changes.
	MarkSignOut bool

	// BindSession ties each new session to a host-only companion cookie and
	// refuses a session cookie that arrives without its own (see binding.go).
	// Sessions issued unbound are refused once it is on, so turning it on signs
	// everyone in again.
	BindSession bool

	// RevokeRefreshToken has Logout revoke the session's refresh token at the
	// provider's revocation endpoint, so a copied cookie cannot refresh after
	// sign-out. Without a revocation endpoint in discovery it does nothing.
	RevokeRefreshToken bool

	// OnSignIn runs once a sign-in has completed and its session is saved, with
	// that session. Left nil, nothing runs.
	OnSignIn func(context.Context, Session)

	// ResolveAdmin asks IAM whether the person holding an access token is an
	// admin. The server sets it in iam mode; left nil (or failing) the session
	// keeps no IAM answer and admin falls back to the token's groups claim.
	ResolveAdmin func(ctx context.Context, accessToken string) (bool, error)
	// Logger reports a failed ResolveAdmin; nil is silent.
	Logger *zap.Logger

	// BasePath is the path prefix the panel is served under ("" or "/a/b", as
	// config.ServerConfig.Base returns it). It scopes every cookie this
	// authenticator sets or clears, and keeps the post-login landing under the
	// panel. Set it before serving.
	BasePath string

	// revocationURL is the provider's RFC 7009 endpoint, from discovery.
	revocationURL string
	// httpClient makes the revocation call; nil is http.DefaultClient.
	httpClient *http.Client
	// logoutVerifier checks back-channel logout tokens (backchannel.go).
	logoutVerifier *oidc.IDTokenVerifier
	// signedOut is the back-channel deny-list of provider session IDs.
	signedOut *sidDenyList

	// Collapses concurrent and near-concurrent access-token refreshes, so one
	// refresh token is never redeemed twice. See refreshShared.
	refreshGroupFields
}

// New builds an Authenticator, discovering the provider's endpoints.
//
// It fails rather than degrading: in AuthModeIAM there is no other way to
// identify a user, so a panel that started without working OIDC would serve
// every request unauthenticated.
func New(ctx context.Context, cfg config.OIDCConfig) (*Authenticator, error) {
	if strings.TrimSpace(cfg.IssuerURL) == "" {
		return nil, fmt.Errorf("oidc.issuer_url is required when server.auth_mode is %q", config.AuthModeIAM)
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("oidc.client_id is required when server.auth_mode is %q", config.AuthModeIAM)
	}
	if strings.TrimSpace(cfg.CookieKey) == "" {
		return nil, fmt.Errorf("oidc.cookie_key is required when server.auth_mode is %q "+
			"(base64 of 32 random bytes, e.g. openssl rand -base64 32)", config.AuthModeIAM)
	}

	provider, err := oidc.NewProvider(ctx, strings.TrimRight(cfg.IssuerURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	codec, err := NewCookieCodec(cfg.CookieKey)
	if err != nil {
		return nil, err
	}

	scopes := append([]string{oidc.ScopeOpenID}, cfg.Scopes...)

	var discovered struct {
		Revocation string `json:"revocation_endpoint"`
	}
	_ = provider.Claims(&discovered)

	return &Authenticator{
		cfg:           cfg,
		revocationURL: strings.TrimSpace(discovered.Revocation),
		// A logout token has no nonce and need not carry exp; the back-channel
		// handler checks its freshness itself.
		logoutVerifier:     provider.Verifier(&oidc.Config{ClientID: cfg.ClientID, SkipExpiryCheck: true}),
		signedOut:          newSIDDenyList(),
		refreshGroupFields: refreshGroupFields{refreshMemo: newRefreshMemo()},
		provider:           provider,
		verifier:           provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		codec:              codec,
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       dedupe(scopes),
		},
	}, nil
}

// Router is where the login endpoints are mounted: the echo instance itself, or
// a group carrying the panel's base path.
type Router interface {
	GET(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
	POST(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route
}

// RegisterRoutes mounts the login endpoints.
//
// They sit OUTSIDE the API group on purpose: that group carries the static
// bearer-token middleware, which would reject the provider's callback (it
// arrives with no Authorization header and cannot be given one).
func (a *Authenticator) RegisterRoutes(e Router) {
	e.GET("/auth/login", a.Login)
	e.GET("/auth/callback", a.Callback)
	// POST only: signing out revokes storage credentials, and a GET can be
	// triggered cross-site by an image tag.
	e.POST("/auth/logout", a.Logout)
	e.GET("/auth/me", a.Me)
	// The provider's server-to-server call when a session ends there
	// (OpenID Connect Back-Channel Logout 1.0).
	e.POST("/auth/backchannel-logout", a.BackchannelLogout)
}

// Login starts the authorization-code flow.
//
// ?next= names where the person was going, so a link into the panel survives
// the round trip through the provider. It is kept only if it is a relative
// path under the panel (SafeReturnPath); anything else is dropped and the
// sign-in lands on the usual page. It travels in the state cookie, which the
// browser holds and the provider never sees.
func (a *Authenticator) Login(c echo.Context) error {
	state, err := randomToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not start login")
	}
	nonce, err := randomToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not start login")
	}

	value := state + "." + nonce
	if next := a.SafeReturnPath(c.QueryParam("next")); next != "" {
		value += "." + base64.RawURLEncoding.EncodeToString([]byte(next))
	}
	if name, hostWide := a.stateCookie(); hostWide {
		a.setHostCookie(c, name, value, stateTTL)
	} else {
		a.setCookie(c, name, value, stateTTL)
	}

	return c.Redirect(http.StatusFound, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce)))
}

// Callback completes the flow and sets the session cookie.
func (a *Authenticator) Callback(c echo.Context) error {
	stateName, hostWide := a.stateCookie()
	states := requestCookies(c.Request(), stateName)
	// Clear it immediately: a state is single-use, and leaving it set would let a
	// replayed callback reuse it.
	if hostWide {
		a.clearHostCookie(c, stateName)
	} else {
		a.clearCookie(c, stateName)
	}
	if len(states) != 1 || states[0] == "" {
		// Two state cookies means one was set by someone else (cookie tossing).
		return echo.NewHTTPError(http.StatusBadRequest, "login state is missing or expired; start again at "+a.BasePath+"/auth/login")
	}

	parts := strings.Split(states[0], ".")
	if len(parts) < 2 || len(parts) > 3 {
		return echo.NewHTTPError(http.StatusBadRequest, "login state is malformed")
	}
	expectedState, expectedNonce := parts[0], parts[1]
	next := ""
	if len(parts) == 3 {
		if raw, err := base64.RawURLEncoding.DecodeString(parts[2]); err == nil {
			// Checked again: the cookie is the browser's to edit.
			next = a.SafeReturnPath(string(raw))
		}
	}
	if c.QueryParam("state") != expectedState {
		return echo.NewHTTPError(http.StatusBadRequest, "login state does not match")
	}
	if desc := c.QueryParam("error"); desc != "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "identity provider refused the login: "+desc)
	}

	ctx := c.Request().Context()
	token, err := a.oauth.Exchange(ctx, c.QueryParam("code"))
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "could not exchange the authorization code")
	}

	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "the identity provider returned no ID token")
	}
	idToken, err := a.verifier.Verify(ctx, rawID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "the ID token failed verification")
	}
	if idToken.Nonce != expectedNonce {
		return echo.NewHTTPError(http.StatusUnauthorized, "the ID token nonce does not match")
	}

	session, err := a.sessionFromToken(idToken, rawID, token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}
	session = a.withIAMAdmin(ctx, session)
	if a.BindSession {
		if session.Binding, err = randomToken(); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "could not persist the session")
		}
	}
	if err := a.Save(c, session); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not persist the session")
	}
	a.expireLegacyCookies(c, stateCookieName)
	if a.OnSignIn != nil {
		a.OnSignIn(ctx, session)
	}

	if next != "" {
		return c.Redirect(http.StatusFound, next)
	}
	return c.Redirect(http.StatusFound, a.postLoginRedirect())
}

// maxReturnPath bounds ?next=, which rides in a cookie.
const maxReturnPath = 1024

// SafeReturnPath returns next as a path and query under the panel's base path,
// or "" when it is anything else.
//
// What it refuses is what turns a return path into an open redirect or a hop
// into another app on a shared host: a scheme or host ("https://evil",
// "//evil", "/\evil"), a path outside the base ("/iam/…" from "/storage"),
// dot segments or doubled slashes that would resolve outside it, encoded or
// not, control characters, and the sign-in endpoints themselves, which would
// loop. The fragment is dropped; the browser keeps its own.
func (a *Authenticator) SafeReturnPath(next string) string {
	if next == "" || len(next) > maxReturnPath || !strings.HasPrefix(next, "/") ||
		strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\") {
		return ""
	}
	for _, r := range next {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return ""
	}
	// Clean resolves "." and ".." and collapses "//"; any difference means the
	// path said something other than what it looks like.
	if cleaned := path.Clean(u.Path); cleaned != strings.TrimSuffix(u.Path, "/") && cleaned != u.Path {
		return ""
	}
	if strings.Contains(u.Path, "//") {
		return ""
	}
	base := a.BasePath
	if base != "" && u.Path != base && !strings.HasPrefix(u.Path, base+"/") {
		return ""
	}
	if u.Path == base+"/auth" || strings.HasPrefix(u.Path, base+"/auth/") {
		return ""
	}
	out := &url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
	return out.String()
}

// postLoginRedirect is where a completed sign-in lands. A root-relative
// setting is taken as relative to the panel, so the default "/" lands on the
// panel's own home under a base path rather than on whatever owns the host's
// root. A value already under the base, or an absolute URL, is used as is.
func (a *Authenticator) postLoginRedirect() string {
	redirect := a.cfg.PostLoginRedirect
	if redirect == "" {
		redirect = "/"
	}
	if a.BasePath == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		return redirect
	}
	if redirect == a.BasePath || strings.HasPrefix(redirect, a.BasePath+"/") {
		return redirect
	}
	return a.BasePath + redirect
}

// Logout clears the session cookie. It does not end the session at the provider:
// a shared-browser logout is the operator's policy call, and doing it here would
// sign the user out of every application on the same realm.
//
// It does, however, revoke the storage credentials this console obtained on the
// person's behalf. Without that, signing out leaves a credential live for hours
// — a real one, usable from anywhere, that the browser no longer shows anyone.
// OnLogout is best effort: a revoke that fails must not leave the person unable
// to sign out, and the credential still expires on its own.
func (a *Authenticator) Logout(c echo.Context) error {
	if session, ok := a.Load(c); ok {
		if a.OnLogout != nil {
			a.OnLogout(c.Request().Context(), session)
		}
		if a.RevokeRefreshToken {
			a.revokeRefreshToken(c.Request().Context(), session)
		}
	}
	a.clearCookie(c, a.cookieName())
	if a.BindSession {
		a.clearHostCookie(c, a.bindCookieName())
	}
	a.expireLegacyCookies(c)
	if a.MarkSignOut {
		a.setCookie(c, signedOutCookieName, "1", signedOutTTL)
	}
	return c.NoContent(http.StatusNoContent)
}

// takeSignOutMarker reports whether the browser carries the sign-out marker,
// clearing it if so: it speaks for one visit only.
func (a *Authenticator) takeSignOutMarker(c echo.Context) bool {
	if ck, err := c.Cookie(signedOutCookieName); err != nil || ck.Value == "" {
		return false
	}
	a.clearCookie(c, signedOutCookieName)
	return true
}

// Me reports the current session, so the SPA can decide what to render. It
// deliberately never returns the tokens.
func (a *Authenticator) Me(c echo.Context) error {
	session, ok := a.Load(c)
	if !ok {
		body := map[string]any{"authenticated": false}
		if a.takeSignOutMarker(c) {
			// The person signed out on purpose, so the SPA may sign in again
			// at once instead of waiting out its loop guard.
			body["signed_out"] = true
		}
		return c.JSON(http.StatusUnauthorized, body)
	}
	a.takeSignOutMarker(c)
	return c.JSON(http.StatusOK, map[string]any{
		"authenticated": true,
		"subject":       session.Subject,
		"email":         session.Email,
		"name":          session.Display(),
		"groups":        session.Groups,
		"is_admin":      session.IsAdmin(a.cfg.AdminGroups),
		"real_is_admin": session.IsRealAdmin(a.cfg.AdminGroups),
		"act_as":        session.ActAs,
	})
}

// Load reads the session cookie, refreshing the access token underneath it when
// that token has lapsed.
//
// Without the refresh the sign-in would end whenever the access token did —
// five minutes on a default Keycloak — which is not a session, it is a
// stopwatch. The sign-in itself ends at SessionExpiresAt.
func (a *Authenticator) Load(c echo.Context) (Session, bool) {
	values := requestCookies(c.Request(), a.cookieName())
	if len(values) > 1 {
		// The panel only ever sets one; another came from somewhere else.
		a.refuseTossed(c)
		return Session{}, false
	}
	if len(values) == 0 || values[0] == "" {
		return Session{}, false
	}
	session, err := a.codec.Decode(values[0])
	if err != nil || session.Expired() {
		return Session{}, false
	}
	if a.BindSession && !a.bindingHolds(c.Request(), session) {
		// Not this browser's sign-in (a tossed cookie, cleared wherever it
		// could have been set), or one issued before binding was on.
		a.refuseTossed(c)
		return Session{}, false
	}
	if a.signedOut.has(session.SID) {
		// The provider ended this session (back-channel logout).
		a.clearCookie(c, a.cookieName())
		return Session{}, false
	}
	if !session.TokenExpired() {
		return session, true
	}

	refreshed, err := a.refreshShared(c.Request().Context(), session)
	if err != nil {
		// The refresh token is spent or revoked: the sign-in is over, and the
		// user has to authenticate again.
		return Session{}, false
	}
	// Persist the rotated tokens, or the next request refreshes again — and with
	// refresh-token rotation on, replaying a spent token can invalidate the whole
	// chain at the provider.
	if err := a.Save(c, refreshed); err != nil {
		return refreshed, true
	}
	return refreshed, true
}

// refresh exchanges the refresh token for a new access token, keeping the
// identity claims already established at sign-in.
func (a *Authenticator) refresh(ctx context.Context, prior Session) (Session, error) {
	if prior.RefreshToken == "" {
		return Session{}, fmt.Errorf("session has no refresh token")
	}
	token, err := a.oauth.TokenSource(ctx, &oauth2.Token{
		RefreshToken: prior.RefreshToken,
		Expiry:       prior.ExpiresAt,
	}).Token()
	if err != nil {
		return Session{}, fmt.Errorf("refresh access token: %w", err)
	}

	next := prior
	next.AccessToken = token.AccessToken
	next.ExpiresAt = token.Expiry
	if token.RefreshToken != "" {
		// Providers with rotation return a new one; keeping the old would break
		// the next refresh.
		next.RefreshToken = token.RefreshToken
	}
	// Keycloak issues a fresh ID token on refresh. It replaces the stored hint
	// only once verified, so a refresh response cannot plant one.
	if rawID, ok := token.Extra("id_token").(string); ok && rawID != "" && a.verifier != nil {
		if _, err := a.verifier.Verify(ctx, rawID); err == nil {
			next.IDToken = rawID
		}
	}
	// The person's groups may have changed since sign-in; the new token is the
	// moment to ask IAM again.
	return a.withIAMAdmin(ctx, next), nil
}

// withIAMAdmin records IAM's verdict on whether the session's person is an admin.
//
// A failed lookup must not sign anyone out or lock an admin out: it leaves
// whatever the session already knew (nothing at sign-in, so admin falls back to
// the groups claim; the previous answer on a refresh).
func (a *Authenticator) withIAMAdmin(ctx context.Context, s Session) Session {
	if a.ResolveAdmin == nil {
		return s
	}
	admin, err := a.ResolveAdmin(ctx, s.AccessToken)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn("could not ask IAM whether the person is an admin",
				zap.String("subject", s.Subject), zap.Error(err))
		}
		return s
	}
	s.IAMAdmin = &admin
	return s
}

// Save seals a session into the cookie.
//
// The cookie's lifetime follows the SIGN-IN, not the access token: a cookie that
// expired with the access token would drop the refresh token on the floor and
// end the session minutes after it began.
//
// A session that will not fit is saved without its ID token rather than not at
// all. The ID token is only a hint for a sign-out at the provider; the person
// cannot use the panel without the rest.
func (a *Authenticator) Save(c echo.Context, s Session) error {
	value, err := a.codec.Encode(s)
	if errors.Is(err, ErrCookieTooLarge) && s.IDToken != "" {
		if a.Logger != nil {
			a.Logger.Warn("session cookie too large with the ID token; keeping the session without it",
				zap.String("subject", s.Subject))
		}
		s.IDToken = ""
		value, err = a.codec.Encode(s)
	}
	if err != nil {
		return err
	}
	ttl := time.Until(s.SessionExpiresAt)
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	a.setCookie(c, a.cookieName(), value, ttl)
	if a.BindSession && s.Binding != "" {
		a.setHostCookie(c, a.bindCookieName(), s.Binding, ttl)
	}
	return nil
}

// AdminGroups exposes the configured admin groups to callers that need them.
func (a *Authenticator) AdminGroups() []string { return a.cfg.AdminGroups }

// sessionFromToken projects the verified claims into a Session.
func (a *Authenticator) sessionFromToken(idToken *oidc.IDToken, rawID string, token *oauth2.Token) (Session, error) {
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return Session{}, fmt.Errorf("could not read the ID token claims")
	}

	session := Session{
		Subject:      idToken.Subject,
		Email:        stringClaim(claims, "email"),
		Name:         stringClaim(claims, "name"),
		Groups:       stringsClaim(claims, a.groupsClaim()),
		SID:          stringClaim(claims, "sid"),
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      rawID,
		ExpiresAt:    token.Expiry,
		// How long the person stays signed in, independent of how often the
		// access token underneath is rotated.
		SessionExpiresAt: time.Now().Add(a.sessionTTL()),
	}
	if session.Email == "" {
		// Some providers only put the address in preferred_username.
		session.Email = stringClaim(claims, "preferred_username")
	}
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = idToken.Expiry
	}
	return session, nil
}

func (a *Authenticator) cookieName() string {
	if a.cfg.CookieName != "" {
		return a.cfg.CookieName
	}
	return config.DefaultCookieName
}

// sessionTTL is how long a sign-in lasts before re-authentication.
func (a *Authenticator) sessionTTL() time.Duration {
	if a.cfg.SessionTTL > 0 {
		return a.cfg.SessionTTL
	}
	return DefaultSessionTTL
}

func (a *Authenticator) groupsClaim() string {
	if a.cfg.GroupsClaim != "" {
		return a.cfg.GroupsClaim
	}
	return config.DefaultGroupsClaim
}

// cookiePath scopes every cookie to the panel's own path, so a browser sends
// none of them to the other apps sharing its host. It is the same value
// everywhere a cookie is set or cleared: a clear with another Path leaves the
// cookie in place, and sign-out would silently keep the session.
func (a *Authenticator) cookiePath() string {
	if a.BasePath == "" {
		return "/"
	}
	return a.BasePath
}

func (a *Authenticator) setCookie(c echo.Context, name, value string, ttl time.Duration) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     a.cookiePath(),
		HttpOnly: true, // the SPA never needs to read it, and script access is how session cookies leak
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode, // Lax, not Strict: the provider redirect is a cross-site GET
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

func (a *Authenticator) clearCookie(c echo.Context, name string) {
	a.clearCookieAt(c, name, a.cookiePath())
}

func (a *Authenticator) clearCookieAt(c echo.Context, name, path string) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// expireLegacyCookies clears the session cookie a panel served at the host's
// root left behind, once the panel has moved under a base path.
//
// That cookie has Path=/, so it would otherwise ride along on every request to
// every app on the host for the rest of its lifetime (12 hours by default) —
// exactly what scoping the cookie to the base path is meant to stop. Both the
// configured name and the default are cleared, since a move under a base path
// usually renames the cookie too. A no-op at the root, where Path=/ is the
// current cookie.
func (a *Authenticator) expireLegacyCookies(c echo.Context, extra ...string) {
	if a.BasePath == "" {
		return
	}
	for _, name := range dedupe(append([]string{a.cookieName(), config.DefaultCookieName}, extra...)) {
		a.clearCookieAt(c, name, "/")
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func stringClaim(claims map[string]any, key string) string {
	v, _ := claims[key].(string)
	return strings.TrimSpace(v)
}

// stringsClaim reads a claim that may be a list or a single string; providers
// differ, and a group claim delivered as a bare string is common.
func stringsClaim(claims map[string]any, key string) []string {
	switch v := claims[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
