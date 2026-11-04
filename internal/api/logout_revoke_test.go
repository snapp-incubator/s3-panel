package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// TestSignOutRevokesInEveryRegion (proposal 015 A6): the revoke names no
// region, so IAM's POST /sts/logout reaches every region the person holds a
// grant in, not just this instance's. It names the console client, and never
// acts as a debug user.
func TestSignOutRevokesInEveryRegion(t *testing.T) {
	var got *http.Request
	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sts.Close()

	client, err := control.New(sts.URL+"/s3", sts.URL+"/sts", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Config:      config.Config{Server: config.ServerConfig{AuthMode: config.AuthModeIAM, Region: "teh-1"}},
		control:     client,
		logger:      zap.NewNop(),
		credentials: newCredentialCache(t.Context()),
	}
	cred := &control.Credentials{Expiration: time.Now().Add(time.Hour)}
	s.credentials.put("u-alice|teh-2|b|read", cred)

	s.revokeOnLogout(t.Context(), auth.Session{Subject: "u-alice", AccessToken: "tok", ActAs: "qa-bob@debug.iam.local"})

	if got == nil {
		t.Fatal("sign-out revoked nothing")
	}
	q := got.URL.Query()
	if got.Method != http.MethodPost || got.URL.Path != "/sts/logout" || q.Get("client") != "console" {
		t.Errorf("revoke = %s %s, want POST /sts/logout?client=console", got.Method, got.URL)
	}
	if q.Has("region") || got.Header.Get("region") != "" {
		t.Errorf("revoke names region %q, which limits it to that one region", q.Get("region"))
	}
	if got.Header.Get(control.ActAsHeader) != "" {
		t.Error("revoke acted as the debug user, which would revoke other admins' credentials")
	}
	if _, ok := s.credentials.get("u-alice|teh-2|b|read"); ok {
		t.Error("the person's cached credentials survived sign-out")
	}
}
