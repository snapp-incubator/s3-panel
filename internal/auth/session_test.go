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
		Subject:          "abc-123",
		Email:            "alice@example.com",
		Name:             "Alice",
		Groups:           []string{"payments", "/Cloud"},
		AccessToken:      "token-value",
		RefreshToken:     "refresh-value",
		ExpiresAt:        time.Now().Add(5 * time.Minute).Truncate(time.Second),
		SessionExpiresAt: time.Now().Add(12 * time.Hour).Truncate(time.Second),
	}

	encoded, err := codec.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// The cookie must not carry the session in the clear: it holds an access
	// token, and anything readable in a cookie is readable by whoever gets it.
	if strings.Contains(encoded, "alice@example.com") ||
		strings.Contains(encoded, "token-value") ||
		strings.Contains(encoded, "refresh-value") {
		t.Fatal("cookie value contains plaintext session data")
	}

	got, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Subject != want.Subject || got.Email != want.Email ||
		got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
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

// TestSessionExpiryIsIndependentOfTokenExpiry pins the distinction that keeps a
// user signed in.
//
// The access token lapses in minutes on a default Keycloak. If that ended the
// sign-in, the user would be bounced to the login screen every few minutes; the
// token is refreshed underneath a session that runs much longer.
func TestSessionExpiryIsIndependentOfTokenExpiry(t *testing.T) {
	// The common case mid-session: token stale, sign-in still good. This must
	// refresh, not sign the user out.
	stale := Session{
		ExpiresAt:        time.Now().Add(-time.Minute),
		SessionExpiresAt: time.Now().Add(11 * time.Hour),
	}
	if !stale.TokenExpired() {
		t.Error("a lapsed access token was reported fresh")
	}
	if stale.Expired() {
		t.Error("a lapsed access token ended the sign-in; the user would be logged out every few minutes")
	}

	fresh := Session{
		ExpiresAt:        time.Now().Add(4 * time.Minute),
		SessionExpiresAt: time.Now().Add(11 * time.Hour),
	}
	if fresh.TokenExpired() || fresh.Expired() {
		t.Error("a fresh session was reported expired")
	}

	// Inside the leeway, so a request in flight is not signed with a token that
	// expires mid-call.
	if !(Session{ExpiresAt: time.Now().Add(5 * time.Second)}).TokenExpired() {
		t.Error("a token inside the refresh leeway was reported fresh")
	}

	// The sign-in itself ending is what actually logs the user out.
	if !(Session{SessionExpiresAt: time.Now().Add(-time.Second)}).Expired() {
		t.Error("an ended sign-in was reported valid")
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

// TestCookieStaysUnderBrowserLimit is the regression test for a failure with no
// symptom: a cookie over ~4096 bytes is dropped by the browser silently. The
// server sets it, the login looks like it worked, and the user lands back on the
// sign-in page with nothing in any log to explain why.
//
// Two Keycloak JWTs overflow that on their own, which is why the payload is
// compressed before sealing.
func TestCookieStaysUnderBrowserLimit(t *testing.T) {
	codec, err := NewCookieCodec(testKey(t))
	if err != nil {
		t.Fatalf("NewCookieCodec: %v", err)
	}

	// Realistically sized Keycloak tokens: a signed JWT with a fair few claims.
	session := Session{
		Subject:          "6885c70e-a794-4438-9c44-62ccfe845dea",
		Email:            "alice@example.com",
		Name:             "Alice Ahmadi",
		Groups:           []string{"payments-demo", "Cloud", "sre"},
		AccessToken:      fakeJWT(1400),
		RefreshToken:     fakeJWT(1600),
		ExpiresAt:        time.Now().Add(5 * time.Minute),
		SessionExpiresAt: time.Now().Add(12 * time.Hour),
	}

	encoded, err := codec.Encode(session)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) > maxCookieBytes {
		t.Errorf("cookie is %d bytes, over the %d-byte browser limit", len(encoded), maxCookieBytes)
	}

	got, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.AccessToken != session.AccessToken || got.RefreshToken != session.RefreshToken {
		t.Error("compression lost token data")
	}
}

// TestEncodeRefusesOversizedSession: if a provider's tokens are large enough to
// overflow even compressed, fail loudly at sign-in rather than handing the
// browser a cookie it will discard.
func TestEncodeRefusesOversizedSession(t *testing.T) {
	codec, err := NewCookieCodec(testKey(t))
	if err != nil {
		t.Fatalf("NewCookieCodec: %v", err)
	}
	// Random (incompressible) payloads well past the ceiling.
	if _, err := codec.Encode(Session{
		AccessToken:  incompressibleToken(t, 6000),
		RefreshToken: incompressibleToken(t, 6000),
	}); err == nil {
		t.Error("Encode produced a cookie the browser would silently drop")
	}
}

// fakeJWT builds a compressible base64-ish blob of roughly n bytes, which is
// what a real JWT looks like to the compressor.
func fakeJWT(n int) string {
	const alphabet = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.abcdefghijklmnopqrstuvwxyz0123456789-_"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[i%len(alphabet)]
	}
	return string(b)
}

// incompressibleToken builds a random blob, so the size check cannot be
// satisfied by compression. (randomToken is the production helper in oidc.go.)
func incompressibleToken(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
