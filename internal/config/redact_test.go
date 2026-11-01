package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRedactedHidesEverySecret: the startup log prints the whole configuration,
// so every secret field must be masked in it. A new secret field added to the
// config without being added to redacted() fails here only if it is added to
// this list too — keep the two in step.
func TestRedactedHidesEverySecret(t *testing.T) {
	const secret = "s3cr3t-value-that-must-not-be-logged"
	var c Config
	c.Server.AuthToken = secret
	c.ObjectStorage.AccessKeyAdmin = secret
	c.ObjectStorage.SecretKeyAdmin = secret
	c.ObjectStorage.STSClientKey = secret
	c.OIDC.ClientSecret = secret
	c.OIDC.CookieKey = secret
	c.Panel.APIKey = secret

	out, err := json.Marshal(redacted(c))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), secret) {
		t.Fatalf("a secret survived redaction:\n%s", out)
	}
	if n := strings.Count(string(out), "[redacted]"); n != 7 {
		t.Fatalf("redacted %d fields, want 7:\n%s", n, out)
	}
	// The original is untouched: redaction is for the log only.
	if c.OIDC.ClientSecret != secret {
		t.Fatal("redacted() modified the live configuration")
	}
}
