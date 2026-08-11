package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func TestCookieRoundTrip(t *testing.T) {
	codec, err := NewCookieCodec(testKey(t))
	if err != nil {
		t.Fatalf("NewCookieCodec: %v", err)
	}

	want := Session{
		Subject:     "abc-123",
		Email:       "alice@example.com",
		Name:        "Alice",
		Groups:      []string{"payments", "/Cloud"},
		AccessToken: "token-value",
		ExpiresAt:   time.Now().Add(time.Hour).Truncate(time.Second),
	}

	encoded, err := codec.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// The cookie must not carry the session in the clear: it holds an access
	// token, and anything readable in a cookie is readable by whoever gets it.
	if strings.Contains(encoded, "alice@example.com") || strings.Contains(encoded, "token-value") {
		t.Fatal("cookie value contains plaintext session data")
	}

	got, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Subject != want.Subject || got.Email != want.Email || got.AccessToken != want.AccessToken {
		t.Errorf("round trip lost data: %+v", got)
	}
	if len(got.Groups) != 2 {
		t.Errorf("groups = %v", got.Groups)
	}
}

// TestCookieRejectsTampering: a forged or truncated cookie must fail to open
// rather than yield a partially-trusted session.
func TestCookieRejectsTampering(t *testing.T) {
	codec, err := NewCookieCodec(testKey(t))
	if err != nil {
		t.Fatalf("NewCookieCodec: %v", err)
	}
	encoded, err := codec.Encode(Session{Subject: "abc"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	cases := map[string]string{
		"flipped byte": flipCiphertextByte(t, encoded),
		"truncated":    encoded[:len(encoded)/2],
		"empty":        "",
		"not base64":   "!!!not-base64!!!",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(value); err == nil {
				t.Errorf("Decode accepted a %s cookie", name)
			}
		})
	}
}

// TestCookieRejectsForeignKey: a cookie sealed with a different key must not
// open, so rotating the key invalidates old sessions instead of trusting them.
func TestCookieRejectsForeignKey(t *testing.T) {
	first, _ := NewCookieCodec(testKey(t))
	second, _ := NewCookieCodec(testKey(t))

	encoded, err := first.Encode(Session{Subject: "abc"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := second.Decode(encoded); err == nil {
		t.Error("a cookie sealed with another key was accepted")
	}
}

func TestNewCookieCodecRejectsBadKeys(t *testing.T) {
	for name, key := range map[string]string{
		"empty":      "",
		"not base64": "%%%",
		"too short":  base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCookieCodec(key); err == nil {
				t.Errorf("NewCookieCodec accepted a %s key", name)
			}
		})
	}
}

func TestSessionExpired(t *testing.T) {
	if (Session{ExpiresAt: time.Now().Add(time.Hour)}).Expired() {
		t.Error("a session valid for another hour was reported expired")
	}
	if !(Session{ExpiresAt: time.Now().Add(-time.Minute)}).Expired() {
		t.Error("a lapsed session was reported valid")
	}
	// Inside the leeway, so a request in flight is not signed with a token that
	// expires mid-call.
	if !(Session{ExpiresAt: time.Now().Add(5 * time.Second)}).Expired() {
		t.Error("a session inside the refresh leeway was reported valid")
	}
	if (Session{}).Expired() {
		t.Error("a session with no expiry should not be treated as expired")
	}
}

func TestSessionIsAdmin(t *testing.T) {
	cases := []struct {
		name   string
		groups []string
		admin  []string
		want   bool
	}{
		{"exact", []string{"Cloud"}, []string{"Cloud"}, true},
		// Providers report group paths with a leading slash; operators configure
		// the bare name.
		{"provider path form", []string{"/Cloud"}, []string{"Cloud"}, true},
		{"case insensitive", []string{"cloud"}, []string{"Cloud"}, true},
		{"among others", []string{"payments", "/Cloud"}, []string{"Cloud"}, true},
		{"not a member", []string{"payments"}, []string{"Cloud"}, false},
		{"no groups", nil, []string{"Cloud"}, false},
		{"none configured", []string{"Cloud"}, nil, false},
		{"near miss", []string{"CloudOps"}, []string{"Cloud"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (Session{Groups: c.groups}).IsAdmin(c.admin); got != c.want {
				t.Errorf("IsAdmin(%v, %v) = %v, want %v", c.groups, c.admin, got, c.want)
			}
		})
	}
}

func TestStringsClaim(t *testing.T) {
	cases := map[string]struct {
		claims map[string]any
		want   int
	}{
		"list":          {map[string]any{"groups": []any{"a", "b"}}, 2},
		"single string": {map[string]any{"groups": "a"}, 1},
		"empty string":  {map[string]any{"groups": ""}, 0},
		"absent":        {map[string]any{}, 0},
		"wrong type":    {map[string]any{"groups": 42}, 0},
		"mixed types":   {map[string]any{"groups": []any{"a", 42, "b"}}, 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := stringsClaim(c.claims, "groups"); len(got) != c.want {
				t.Errorf("stringsClaim = %v, want %d entries", got, c.want)
			}
		})
	}
}

// flipCiphertextByte alters the sealed bytes themselves.
//
// Flipping a character of the base64 text is not enough: the final character
// carries unused bits, so a change there can decode to identical bytes and the
// seal still opens. Decoding first makes the tamper real.
func flipCiphertextByte(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	raw[len(raw)/2] ^= 0xFF
	return base64.RawURLEncoding.EncodeToString(raw)
}
