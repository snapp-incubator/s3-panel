package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// revokeTimeout bounds the revocation a sign-out waits for.
const revokeTimeout = 5 * time.Second

// revokeRefreshToken revokes the session's refresh token at the provider
// (RFC 7009), so a copy of the cookie cannot refresh once the person signed
// out. Best effort: a provider that is down must not stop the sign-out, and the
// token still ends with the provider's session.
//
// On Keycloak, revoking an online refresh token also detaches the client's
// session from the person's sign-in. With one client shared by every app of a
// unified console, that ends the console's sign-in for this client, which is
// what the console's own sign-out (it signs out of every app) intends.
func (a *Authenticator) revokeRefreshToken(ctx context.Context, s Session) {
	if a.revocationURL == "" || s.RefreshToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
	defer cancel()
	if err := a.postRevocation(ctx, s.RefreshToken); err != nil && a.Logger != nil {
		a.Logger.Warn("could not revoke the refresh token at sign-out",
			zap.String("subject", s.Subject), zap.Error(err))
	}
}

func (a *Authenticator) postRevocation(ctx context.Context, token string) error {
	form := url.Values{"token": {token}, "token_type_hint": {"refresh_token"}}
	if a.cfg.ClientSecret == "" {
		// A public client identifies itself in the body.
		form.Set("client_id", a.cfg.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.revocationURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a.cfg.ClientSecret != "" {
		// client_secret_basic, encoded as RFC 6749 §2.3.1 says.
		req.SetBasicAuth(url.QueryEscape(a.cfg.ClientID), url.QueryEscape(a.cfg.ClientSecret))
	}
	client := a.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	// RFC 7009 §2.2: 200 whether or not the token was still valid.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revocation endpoint answered %d", resp.StatusCode)
	}
	return nil
}
