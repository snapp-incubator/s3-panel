// Package auth implements the browser login used in AuthModeIAM: a standard
// OIDC authorization-code flow plus an encrypted session cookie.
//
// It is deliberately provider-agnostic. The panel learns who the user is from
// the ID token and nothing else; deciding what that user may reach is the
// control endpoint's job (see internal/iam).
package auth

import (
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
type Session struct {
	Subject     string    `json:"sub"`
	Email       string    `json:"email,omitempty"`
	Name        string    `json:"name,omitempty"`
	Groups      []string  `json:"groups,omitempty"`
	AccessToken string    `json:"access_token,omitempty"`
	IDToken     string    `json:"id_token,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Expired reports whether the session is past its expiry, with a small leeway so
// a request that is in flight when the token lapses is not rejected mid-call.
func (s Session) Expired() bool {
	return !s.ExpiresAt.IsZero() && time.Now().After(s.ExpiresAt.Add(-30*time.Second))
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

// CookieCodec seals a Session into the opaque cookie value and back.
// AES-256-GCM gives confidentiality and integrity in one primitive, so a
// tampered cookie fails to open rather than yielding a forged identity.
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
func (c *CookieCodec) Encode(s Session) (string, error) {
	plaintext, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
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
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return Session{}, fmt.Errorf("open cookie: %w", err)
	}
	var s Session
	if err := json.Unmarshal(plaintext, &s); err != nil {
		return Session{}, fmt.Errorf("unmarshal session: %w", err)
	}
	return s, nil
}
