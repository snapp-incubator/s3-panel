package auth

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// refreshResultTTL is how long the outcome of one refresh is remembered, keyed
// by the refresh token that was spent to obtain it.
//
// It only has to span the gap between a request refreshing the session and the
// browser receiving the Set-Cookie that carries the rotated token — the window
// in which another in-flight request still holds the old cookie. A minute is
// generous for that and far shorter than any refresh-token lifetime.
const refreshResultTTL = time.Minute

// refreshMemo remembers, per spent refresh token, the session it produced.
type refreshMemo struct {
	mu    sync.Mutex
	items map[string]refreshEntry
}

type refreshEntry struct {
	session Session
	at      time.Time
}

func newRefreshMemo() *refreshMemo {
	return &refreshMemo{items: map[string]refreshEntry{}}
}

func (m *refreshMemo) get(key string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.items[key]
	if !ok {
		return Session{}, false
	}
	if time.Since(entry.at) > refreshResultTTL {
		delete(m.items, key)
		return Session{}, false
	}
	return entry.session, true
}

func (m *refreshMemo) put(key string, session Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Swept on write rather than on a timer: entries are only ever added by a
	// refresh, so the map cannot grow while nothing writes to it.
	for k, entry := range m.items {
		if time.Since(entry.at) > refreshResultTTL {
			delete(m.items, k)
		}
	}
	m.items[key] = refreshEntry{session: session, at: time.Now()}
}

// refreshShared exchanges a refresh token at most once, however many requests
// present it.
//
// The SPA fires several calls in parallel (bucket list, user id, config), so
// when the access token lapses they all reach Load at once holding the SAME
// cookie. Left alone, each redeems the same refresh token — and Keycloak's
// default "Revoke Refresh Token" reuse detection invalidates the whole chain on
// the second redemption, ending the session mid-use and bouncing a signed-in
// user to the login screen.
//
// Two things are needed, because the hazard has two shapes:
//
//   - singleflight collapses the genuinely CONCURRENT redemptions into one call,
//     with every caller receiving its result.
//   - the memo covers the requests that arrive just AFTER, still carrying the old
//     cookie because the Set-Cookie carrying the rotated token has not reached
//     the browser yet. They are not concurrent with anything, so singleflight
//     would let them redeem a token that is already spent.
//
// Keyed by the refresh token itself rather than by the subject: it is exactly
// the thing that may be redeemed once, and two browsers signed in as the same
// person hold different ones.
func (a *Authenticator) refreshShared(ctx context.Context, prior Session) (Session, error) {
	key := prior.RefreshToken
	if key == "" {
		return a.refresh(ctx, prior)
	}
	if session, ok := a.refreshMemo.get(key); ok {
		return session, nil
	}

	result, err, _ := a.refreshGroup.Do(key, func() (any, error) {
		// Re-checked inside the flight: a caller can enter Do just as the previous
		// holder of this key finishes and publishes its result.
		if session, ok := a.refreshMemo.get(key); ok {
			return session, nil
		}
		next, err := a.refresh(ctx, prior)
		if err != nil {
			return Session{}, err
		}
		a.refreshMemo.put(key, next)
		return next, nil
	})
	if err != nil {
		return Session{}, err
	}
	return result.(Session), nil
}

// refreshGroupFields is embedded in Authenticator; kept here so the refresh
// machinery reads as one piece.
type refreshGroupFields struct {
	refreshGroup singleflight.Group
	refreshMemo  *refreshMemo
}
