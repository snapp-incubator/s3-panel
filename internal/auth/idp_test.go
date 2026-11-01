package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// fakeIdP is just enough of an OIDC provider for the login round trip:
// discovery, a JWKS, and a token endpoint that signs an ID token carrying the
// nonce the panel sent and a Keycloak-style sid.
type fakeIdP struct {
	*httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	nonce string
	sid   string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key, sid: "sid-123"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.URL,
			"authorization_endpoint":                idp.URL + "/authorize",
			"token_endpoint":                        idp.URL + "/token",
			"jwks_uri":                              idp.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig",
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"token_type":    "Bearer",
			"expires_in":    300,
			"id_token":      idp.idToken(t),
		})
	})
	idp.Server = httptest.NewServer(mux)
	t.Cleanup(idp.Close)
	return idp
}

func (idp *fakeIdP) idToken(t *testing.T) string {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss":   idp.URL,
		"aud":   "panel",
		"sub":   "u-alice",
		"email": "alice@example.com",
		"nonce": idp.nonce,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
	}
	if idp.sid != "" {
		claims["sid"] = idp.sid
	}
	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// newIdPAuthenticator builds a real Authenticator against the fake provider.
func newIdPAuthenticator(t *testing.T, idp *fakeIdP, base string) *Authenticator {
	t.Helper()
	a, err := New(context.Background(), config.OIDCConfig{
		IssuerURL:    idp.URL,
		ClientID:     "panel",
		RedirectURL:  "https://panel.example.com" + base + "/auth/callback",
		CookieKey:    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		CookieName:   config.DefaultCookieName,
		CookieSecure: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.BasePath = base
	return a
}

// signIn runs /auth/login then /auth/callback through e, returning the
// callback's response. loginQuery is appended to the login URL.
func signIn(t *testing.T, e *echo.Echo, idp *fakeIdP, base, loginQuery string) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodGet, base+"/auth/login"+loginQuery, nil))
	if login.Code != http.StatusFound {
		t.Fatalf("login = %d: %s", login.Code, login.Body.String())
	}
	authorize, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	idp.mu.Lock()
	idp.nonce = authorize.Query().Get("nonce")
	idp.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, base+"/auth/callback?code=c&state="+url.QueryEscape(authorize.Query().Get("state")), nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// cookiesNamed returns every Set-Cookie for name, in order.
func cookiesNamed(rec *httptest.ResponseRecorder, name string) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func hasClearAt(cookies []*http.Cookie, path string) bool {
	for _, c := range cookies {
		if c.Path == path && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

func setCookieHeaders(rec *httptest.ResponseRecorder) string {
	return strings.Join(rec.Header().Values("Set-Cookie"), "\n")
}
