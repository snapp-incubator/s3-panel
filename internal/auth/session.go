// Package auth implements the browser login used in AuthModeIAM: a standard
// OIDC authorization-code flow plus an encrypted session cookie.
//
// It is deliberately provider-agnostic. The panel learns who the user is from
// the ID token and nothing else; deciding what that user may reach is the
// control endpoint's job (see internal/iam).
package auth

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Session is what the encrypted cookie carries.
//
// The access token is kept because it is what the panel presents to the control
// endpoint — the endpoint then answers for the person at the keyboard rather
// than for an ambient service credential, so the panel never needs privileges of
// its own.
//
// Two expiries, and the distinction matters. The access token typically lives
// minutes (Keycloak defaults to five), while a person expects to stay signed in
// for a working session. Tying the sign-in to the access token would sign users
// out every few minutes, so the access token is refreshed underneath a session
// that ends at SessionExpiresAt.
type Session struct {
	Subject      string   `json:"sub"`
	Email        string   `json:"email,omitempty"`
	Name         string   `json:"name,omitempty"`
	Groups       []string `json:"groups,omitempty"`
	AccessToken  string   `json:"access_token,omitempty"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	// ExpiresAt is when the ACCESS TOKEN lapses and must be refreshed.
	ExpiresAt time.Time `json:"expires_at"`
	// SessionExpiresAt is when the sign-in itself ends and the user must
	// authenticate again, no matter how many refreshes have happened.
	SessionExpiresAt time.Time `json:"session_expires_at"`
}

// TokenExpired reports whether the access token needs refreshing. The leeway
// keeps a request that is in flight as the token lapses from being signed with
// one that expires mid-call.
func (s Session) TokenExpired() bool {
	return !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt.Add(-30*time.Second))
}

// Expired reports whether the sign-in itself has ended.
func (s Session) Expired() bool {
	return !s.SessionExpiresAt.IsZero() && time.Now().After(s.SessionExpiresAt)
}

// IsAdmin reports whether the session holds one of the configured admin groups.
// Matching ignores case and a leading "/", which some providers include on group
// paths but operators usually omit in config.
func (s Session) IsAdmin(adminGroups []string) bool {
	for _, want := range adminGroups {
		want = strings.TrimPrefix(strings.TrimSpace(want), "/")
		if want == "" {
			continue
		}
		for _, have := range s.Groups {
			if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(have), "/"), want) {
				return true
			}
		}
	}
	return false
}

// Display returns the most human identifier available.
func (s Session) Display() string {
	for _, c := range []string{s.Name, s.Email, s.Subject} {
		if c = strings.TrimSpace(c); c != "" {
			return c
		}
	}
	return "unknown"
}

// maxCookieBytes is the practical per-cookie ceiling browsers enforce. A cookie
// over it is dropped SILENTLY: the server sets it, the browser discards it, and
// every later request looks unauthenticated with nothing in any log to say why.
const maxCookieBytes = 4096

// CookieCodec seals a Session into the opaque cookie value and back.
//
// AES-256-GCM gives confidentiality and integrity in one primitive, so a
// tampered cookie fails to open rather than yielding a forged identity.
//
// The payload is compressed before sealing. It carries two Keycloak JWTs, and
// uncompressed those overflow the 4 KB cookie ceiling on their own — they are
// base64-encoded JSON, which roughly halves.
type CookieCodec struct {
	aead cipher.AEAD
}

// NewCookieCodec builds a codec from a base64-encoded 32-byte key.
func NewCookieCodec(keyB64 string) (*CookieCodec, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyB64))
	if err != nil {
		// Accept URL-safe base64 too, which is what `openssl rand -base64 32`
		// piped through `tr '+/' '-_'` produces.
		key, err = base64.RawURLEncoding.DecodeString(strings.TrimSpace(keyB64))
		if err != nil {
			return nil, fmt.Errorf("decode cookie key: %w", err)
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("cookie key must decode to 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build GCM: %w", err)
	}
	return &CookieCodec{aead: aead}, nil
}

// Encode seals a session.
//
// It refuses to produce a value the browser would drop, because that failure is
// otherwise invisible: the login appears to succeed and the user lands back on
// the sign-in page with no error anywhere.
func (c *CookieCodec) Encode(s Session) (string, error) {
	plaintext, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(plaintext); err != nil {
		return "", fmt.Errorf("compress session: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("compress session: %w", err)
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, compressed.Bytes(), nil)
	encoded := base64.RawURLEncoding.EncodeToString(sealed)

	if len(encoded) > maxCookieBytes {
		return "", fmt.Errorf(
			"session cookie would be %d bytes, over the %d-byte limit browsers enforce; "+
				"the identity provider's tokens are unusually large", len(encoded), maxCookieBytes)
	}
	return encoded, nil
}

// Decode opens a sealed session. Any tampering, truncation or key change surfaces
// as an error rather than a partially-trusted session.
func (c *CookieCodec) Decode(value string) (Session, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Session{}, fmt.Errorf("decode cookie: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return Session{}, fmt.Errorf("cookie is too short to contain a nonce")
	}
	compressed, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return Session{}, fmt.Errorf("open cookie: %w", err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return Session{}, fmt.Errorf("decompress session: %w", err)
	}
	defer zr.Close() //nolint:errcheck // read-only

	plaintext, err := io.ReadAll(io.LimitReader(zr, maxSessionBytes))
	if err != nil {
		return Session{}, fmt.Errorf("decompress session: %w", err)
	}

	var s Session
	if err := json.Unmarshal(plaintext, &s); err != nil {
		return Session{}, fmt.Errorf("unmarshal session: %w", err)
	}
	return s, nil
}

// maxSessionBytes bounds decompression, so a crafted cookie cannot expand into
// an unbounded allocation.
const maxSessionBytes = 1 << 20
