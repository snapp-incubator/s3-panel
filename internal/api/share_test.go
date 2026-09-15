package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// TestShareExpiryIsCappedByTheCredential.
//
// The share expiry is caller-supplied with no upper bound — 24h is accepted —
// while the credential signing it lives at most an hour, and may be a CACHED one
// already close to expiry. Without a cap the user receives a URL that silently
// starts answering 403 minutes later, with nothing to tell them why.
func TestShareExpiryIsCappedByTheCredential(t *testing.T) {
	e := echo.New()
	newCtx := func() echo.Context {
		return e.NewContext(httptest.NewRequest(http.MethodGet, "/object/share", nil), httptest.NewRecorder())
	}

	// s3 mode: the URL is signed with the user's own long-lived keys, so nothing
	// caps it and the caller's expiry stands.
	if got := shareExpiryCap(newCtx()); got != 0 {
		t.Errorf("cap with no injected credential = %v, want 0 (uncapped)", got)
	}

	c := newCtx()
	c.Set(credentialExpiryKey, time.Now().Add(20*time.Minute))
	got := shareExpiryCap(c)
	if got <= 19*time.Minute || got > 20*time.Minute {
		t.Errorf("cap = %v, want roughly what is left of the credential", got)
	}

	// Already lapsed: the handler reports this rather than signing a URL that is
	// dead on arrival.
	c = newCtx()
	c.Set(credentialExpiryKey, time.Now().Add(-time.Second))
	if got := shareExpiryCap(c); got >= 0 {
		t.Errorf("cap = %v for a lapsed credential, want a negative value", got)
	}

	// An endpoint that names no expiry caps nothing; there is nothing to cap by.
	c = newCtx()
	c.Set(credentialExpiryKey, time.Time{})
	if got := shareExpiryCap(c); got != 0 {
		t.Errorf("cap = %v with no stated expiry, want 0", got)
	}
}
