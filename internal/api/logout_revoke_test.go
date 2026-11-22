package api

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// stsRecorder is a control endpoint that records every revoke it receives as
// "<method> <path>?<query> act-as=<header>", answering after delay.
type stsRecorder struct {
	mu    sync.Mutex
	calls []string
	*httptest.Server
}

func newSTSRecorder(t *testing.T, delay time.Duration) *stsRecorder {
	t.Helper()
	rec := &stsRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("region") != "" {
			t.Errorf("a revoke carried a region header: %s", r.URL)
		}
		rec.mu.Lock()
		rec.calls = append(rec.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+" act-as="+r.Header.Get(control.ActAsHeader))
		rec.mu.Unlock()
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (r *stsRecorder) sorted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.calls...)
	sort.Strings(out)
	return out
}

func revokeServer(t *testing.T, sts *stsRecorder) *Server {
	t.Helper()
	client, err := control.New(sts.URL+"/s3", sts.URL+"/sts", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		Config:      config.Config{Server: config.ServerConfig{AuthMode: config.AuthModeIAM, Region: "teh-1"}},
		control:     client,
		logger:      zap.NewNop(),
		credentials: newCredentialCache(t.Context()),
	}
}

// TestSignOutRevokesEverywhereItShould (proposal 018 A6): the person's own
// console credentials in every granted region (no region named), and by name
// in this instance's region and every region they were minted one in here, so
// a region whose grant is already gone is still covered; and each credential
// minted while acting, by session ID, as the debug user — never the debug
// user's credentials wholesale, which would end other admins'.
func TestSignOutRevokesEverywhereItShould(t *testing.T) {
	sts := newSTSRecorder(t, 0)
	s := revokeServer(t, sts)
	session := auth.Session{Subject: "u-alice", AccessToken: "tok", ActAs: "qa-bob@debug.iam.local"}
	cred := func(id string) *control.Credentials {
		return &control.Credentials{SessionID: id, Expiration: time.Now().Add(time.Hour)}
	}
	s.credentials.put("u-alice|teh-2|team--b|read", cred("own-1"))
	s.credentials.put("u-alice|as:qa-bob@debug.iam.local|teh-3|team--c|readwrite", cred("acted-1"))
	s.credentials.put("u-carol|teh-4|team--d|read", cred("carol-1"))

	s.revokeCredentials(t.Context(), session, s.credentials.takeIdentity(session.Subject))

	want := []string{
		"DELETE /sts/sessions/acted-1?region=teh-3 act-as=qa-bob@debug.iam.local",
		"POST /sts/logout?client=console act-as=",
		"POST /sts/logout?client=console&region=teh-1 act-as=",
		"POST /sts/logout?client=console&region=teh-2 act-as=",
	}
	if got := sts.sorted(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("revokes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, ok := s.credentials.get("u-carol|teh-4|team--d|read"); !ok {
		t.Error("another person's cached credential was dropped")
	}
}

// TestSignOutDoesNotWaitForRevocation: an unreachable or slow region must not
// hold the sign-out, and the cached credentials are gone at once.
func TestSignOutDoesNotWaitForRevocation(t *testing.T) {
	sts := newSTSRecorder(t, 3*time.Second)
	s := revokeServer(t, sts)
	s.credentials.put("u-alice|teh-2|team--b|read", &control.Credentials{Expiration: time.Now().Add(time.Hour)})

	start := time.Now()
	s.revokeOnLogout(t.Context(), auth.Session{Subject: "u-alice", AccessToken: "tok"})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("sign-out waited %s for the revoke", elapsed)
	}
	if _, ok := s.credentials.get("u-alice|teh-2|team--b|read"); ok {
		t.Error("the person's cached credential survived sign-out")
	}
	// The revokes still start, detached from the request.
	deadline := time.Now().Add(2 * time.Second)
	for len(sts.sorted()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(sts.sorted()); n < 3 {
		t.Errorf("%d revokes started, want 3 (granted regions, teh-1, teh-2)", n)
	}
}

func TestCachedRegion(t *testing.T) {
	for key, want := range map[string]struct {
		region string
		acting bool
	}{
		"u-alice|teh-1|b|read":              {"teh-1", false},
		"u-alice|as:qa@x|teh-2|b|readwrite": {"teh-2", true},
		"u-alice||b|read":                   {"", false},
	} {
		region, acting := cachedRegion(key, "u-alice")
		if region != want.region || acting != want.acting {
			t.Errorf("cachedRegion(%q) = %q, %v; want %q, %v", key, region, acting, want.region, want.acting)
		}
	}
}
