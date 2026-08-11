package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// stateCookieName holds the CSRF state and nonce between /auth/login and
// /auth/callback. It is short-lived and cleared as soon as the callback runs.
const stateCookieName = "s3panel_oidc_state"

// stateTTL bounds how long a login may sit unfinished.
const stateTTL = 10 * time.Minute

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

	return &Authenticator{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		codec:    codec,
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       dedupe(scopes),
		},
	}, nil
}

// RegisterRoutes mounts the login endpoints.
//
// They sit OUTSIDE the API group on purpose: that group carries the static
// bearer-token middleware, which would reject the provider's callback (it
// arrives with no Authorization header and cannot be given one).
func (a *Authenticator) RegisterRoutes(e *echo.Echo) {
	e.GET("/auth/login", a.Login)
	e.GET("/auth/callback", a.Callback)
	e.POST("/auth/logout", a.Logout)
	e.GET("/auth/logout", a.Logout)
	e.GET("/auth/me", a.Me)
}

// Login starts the authorization-code flow.
func (a *Authenticator) Login(c echo.Context) error {
	state, err := randomToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not start login")
	}
	nonce, err := randomToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not start login")
	}

	a.setCookie(c, stateCookieName, state+"."+nonce, stateTTL)

	return c.Redirect(http.StatusFound, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce)))
}

// Callback completes the flow and sets the session cookie.
func (a *Authenticator) Callback(c echo.Context) error {
	stateCookie, err := c.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "login state is missing or expired; start again at /auth/login")
	}
	// Clear it immediately: a state is single-use, and leaving it set would let a
	// replayed callback reuse it.
	a.clearCookie(c, stateCookieName)

	expectedState, expectedNonce, ok := strings.Cut(stateCookie.Value, ".")
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "login state is malformed")
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

	session, err := a.sessionFromToken(idToken, token)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}
	if err := a.Save(c, session); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not persist the session")
	}

	redirect := a.cfg.PostLoginRedirect
	if redirect == "" {
		redirect = "/"
	}
	return c.Redirect(http.StatusFound, redirect)
}

// Logout clears the session cookie. It does not end the session at the provider:
// a shared-browser logout is the operator's policy call, and doing it here would
// sign the user out of every application on the same realm.
func (a *Authenticator) Logout(c echo.Context) error {
	a.clearCookie(c, a.cookieName())
	return c.NoContent(http.StatusNoContent)
}

// Me reports the current session, so the SPA can decide what to render. It
// deliberately never returns the tokens.
func (a *Authenticator) Me(c echo.Context) error {
	session, ok := a.Load(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]any{"authenticated": false})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"authenticated": true,
		"subject":       session.Subject,
		"email":         session.Email,
		"name":          session.Display(),
		"groups":        session.Groups,
		"is_admin":      session.IsAdmin(a.cfg.AdminGroups),
	})
}

// Load reads the session cookie, refreshing the access token underneath it when
// that token has lapsed.
//
// Without the refresh the sign-in would end whenever the access token did —
// five minutes on a default Keycloak — which is not a session, it is a
// stopwatch. The sign-in itself ends at SessionExpiresAt.
func (a *Authenticator) Load(c echo.Context) (Session, bool) {
	cookie, err := c.Cookie(a.cookieName())
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}
	session, err := a.codec.Decode(cookie.Value)
	if err != nil || session.Expired() {
		return Session{}, false
	}
	if !session.TokenExpired() {
		return session, true
	}

	refreshed, err := a.refresh(c.Request().Context(), session)
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
	return next, nil
}

// Save seals a session into the cookie.
//
// The cookie's lifetime follows the SIGN-IN, not the access token: a cookie that
// expired with the access token would drop the refresh token on the floor and
// end the session minutes after it began.
func (a *Authenticator) Save(c echo.Context, s Session) error {
	value, err := a.codec.Encode(s)
	if err != nil {
		return err
	}
	ttl := time.Until(s.SessionExpiresAt)
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	a.setCookie(c, a.cookieName(), value, ttl)
	return nil
}

// AdminGroups exposes the configured admin groups to callers that need them.
func (a *Authenticator) AdminGroups() []string { return a.cfg.AdminGroups }

// sessionFromToken projects the verified claims into a Session.
func (a *Authenticator) sessionFromToken(idToken *oidc.IDToken, token *oauth2.Token) (Session, error) {
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return Session{}, fmt.Errorf("could not read the ID token claims")
	}

	session := Session{
		Subject:      idToken.Subject,
		Email:        stringClaim(claims, "email"),
		Name:         stringClaim(claims, "name"),
		Groups:       stringsClaim(claims, a.groupsClaim()),
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
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

func (a *Authenticator) setCookie(c echo.Context, name, value string, ttl time.Duration) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true, // the SPA never needs to read it, and script access is how session cookies leak
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode, // Lax, not Strict: the provider redirect is a cross-site GET
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

func (a *Authenticator) clearCookie(c echo.Context, name string) {
	c.SetCookie(&http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
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
