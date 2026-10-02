package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// tokenEndpoint is a provider that rotates its refresh token on every
// redemption and counts how many times it was asked — which is the thing under
// test, because Keycloak's default "Revoke Refresh Token" invalidates the whole
// chain when one is redeemed twice.
func tokenEndpoint(t *testing.T, redemptions *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		n := redemptions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-` + string(rune('0'+n)) +
			`","refresh_token":"rt-` + string(rune('0'+n)) +
			`","token_type":"Bearer","expires_in":300}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testAuthenticator(tokenURL string) *Authenticator {
	return &Authenticator{
		oauth: oauth2.Config{
			ClientID: "panel",
			Endpoint: oauth2.Endpoint{TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		refreshGroupFields: refreshGroupFields{refreshMemo: newRefreshMemo()},
	}
}

// TestConcurrentRefreshRedeemsOnce.
//
// The SPA fires several calls in parallel (bucket list, user id, config), so
// when the access token lapses they all reach Load at once holding the SAME
// cookie. Each redeeming the refresh token independently means the second
// redemption trips the provider's reuse detection, the chain is revoked, and the
// user is bounced to the login screen mid-session.
func TestConcurrentRefreshRedeemsOnce(t *testing.T) {
	var redemptions atomic.Int32
	srv := tokenEndpoint(t, &redemptions)
	a := testAuthenticator(srv.URL)

	prior := Session{
		Subject: "u-alice", RefreshToken: "rt-0",
		ExpiresAt:        time.Now().Add(-time.Minute),
		SessionExpiresAt: time.Now().Add(time.Hour),
	}

	const callers = 8
	var wg sync.WaitGroup
	results := make([]Session, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = a.refreshShared(context.Background(), prior)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if got := redemptions.Load(); got != 1 {
		t.Fatalf("the refresh token was redeemed %d times, want 1", got)
	}
	for i, s := range results {
		if s.AccessToken != results[0].AccessToken {
			t.Errorf("caller %d got a different access token than caller 0", i)
		}
		if s.Subject != "u-alice" {
			t.Errorf("caller %d lost the identity established at sign-in", i)
		}
	}
}

// TestLateRefreshReusesTheResult is the half singleflight alone does not cover.
//
// A request arriving just AFTER the refresh is not concurrent with anything, but
// it still carries the old cookie — the Set-Cookie with the rotated token has
// not reached the browser yet — so left alone it redeems a token that is already
// spent, which is exactly what the provider revokes the chain for.
func TestLateRefreshReusesTheResult(t *testing.T) {
	var redemptions atomic.Int32
	srv := tokenEndpoint(t, &redemptions)
	a := testAuthenticator(srv.URL)

	prior := Session{Subject: "u-alice", RefreshToken: "rt-0", ExpiresAt: time.Now().Add(-time.Minute)}

	first, err := a.refreshShared(context.Background(), prior)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	second, err := a.refreshShared(context.Background(), prior)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}

	if got := redemptions.Load(); got != 1 {
		t.Fatalf("a spent refresh token was redeemed again (%d redemptions)", got)
	}
	if second.AccessToken != first.AccessToken {
		t.Error("the late caller did not receive the refreshed session")
	}

	// The NEW token is a different key, so it refreshes on its own.
	if _, err := a.refreshShared(context.Background(), second); err != nil {
		t.Fatalf("refreshing with the rotated token: %v", err)
	}
	if got := redemptions.Load(); got != 2 {
		t.Errorf("redemptions = %d, want 2 — the rotated token must not be memoized under the old key", got)
	}
}

// TestRefreshMemoExpires: the memo only has to span the gap between refreshing
// and the browser receiving the rotated cookie, and must not hold spent tokens
// for the process lifetime.
func TestRefreshMemoExpires(t *testing.T) {
	memo := newRefreshMemo()
	memo.put("rt-0", Session{Subject: "u-alice"})
	if _, ok := memo.get("rt-0"); !ok {
		t.Fatal("a fresh entry was not served")
	}

	memo.mu.Lock()
	memo.items["rt-0"] = refreshEntry{session: Session{}, at: time.Now().Add(-2 * refreshResultTTL)}
	memo.mu.Unlock()

	if _, ok := memo.get("rt-0"); ok {
		t.Error("a stale entry was served")
	}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	if _, ok := memo.items["rt-0"]; ok {
		t.Error("the stale entry was served but not dropped")
	}
}
