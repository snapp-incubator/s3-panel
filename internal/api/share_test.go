package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

// TestShareExpiryCap pins the cap: the least of what remains of the credential
// that will sign the link and share.max_duration, each applying only when set.
//
// The share expiry is caller-supplied with no upper bound — 24h is accepted —
// while the credential signing it lives at most an hour, and may be a CACHED one
// already close to expiry. Without a cap the user receives a URL that silently
// starts answering 403 minutes later, with nothing to tell them why.
func TestShareExpiryCap(t *testing.T) {
	e := echo.New()
	newCtx := func(expiry *time.Time) echo.Context {
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/object/share", nil), httptest.NewRecorder())
		if expiry != nil {
			c.Set(credentialExpiryKey, *expiry)
		}
		return c
	}
	at := func(d time.Duration) *time.Time { t := time.Now().Add(d); return &t }
	zero := time.Time{}

	// about reports whether got is within a second below want: time passes
	// between building the context and computing the cap.
	about := func(got, want time.Duration) bool { return got <= want && got > want-time.Second }

	cases := []struct {
		name       string
		expiry     *time.Time
		configured time.Duration
		check      func(time.Duration) bool
		want       string
	}{
		// s3 mode: signed with the user's own long-lived keys.
		{"nothing caps it", nil, 0, func(d time.Duration) bool { return d == 0 }, "0 (uncapped)"},
		{"max_duration only", nil, 30 * time.Minute, func(d time.Duration) bool { return d == 30*time.Minute }, "30m"},
		{"negative max_duration is no cap", nil, -time.Minute, func(d time.Duration) bool { return d == 0 }, "0"},
		// An endpoint that names no expiry caps nothing; there is nothing to cap by.
		{"credential with no stated expiry", &zero, 0, func(d time.Duration) bool { return d == 0 }, "0"},
		{"no stated expiry, max_duration", &zero, time.Hour, func(d time.Duration) bool { return d == time.Hour }, "1h"},
		// iam mode.
		{"credential only", at(20 * time.Minute), 0, func(d time.Duration) bool { return about(d, 20*time.Minute) }, "~20m"},
		{"credential shorter than max_duration", at(20 * time.Minute), time.Hour, func(d time.Duration) bool { return about(d, 20*time.Minute) }, "~20m"},
		{"max_duration shorter than credential", at(time.Hour), 15 * time.Minute, func(d time.Duration) bool { return d == 15*time.Minute }, "15m"},
		// Lapsed: the handler reports this rather than signing a URL that is dead
		// on arrival — whatever max_duration says.
		{"lapsed credential", at(-time.Second), 0, func(d time.Duration) bool { return d < 0 }, "negative"},
		{"lapsed credential with max_duration", at(-time.Second), time.Hour, func(d time.Duration) bool { return d < 0 }, "negative"},
		// A presigned lifetime is whole seconds; less than one cannot be signed.
		{"under a second left", at(500 * time.Millisecond), 0, func(d time.Duration) bool { return d < 0 }, "negative"},
	}
	for _, tc := range cases {
		if got := shareExpiryCap(newCtx(tc.expiry), tc.configured); !tc.check(got) {
			t.Errorf("%s: cap = %v, want %s", tc.name, got, tc.want)
		}
	}
}

// fakeShareStore answers the two calls the share handler makes, and records the
// cap it was handed. The embedded interface is nil: any other call panics.
type fakeShareStore struct {
	storage.ObjectStorage
	gotMax time.Duration
}

func (f *fakeShareStore) ObjectHead(config.ObjectStorageConfig, storage.ObjectRequestMeta) (storage.ObjectHeadResponse, storage.HTTPErrorWithCode) {
	return storage.ObjectHeadResponse{Exists: true}, storage.HTTPErrorWithCode{}
}

func (f *fakeShareStore) ObjectShare(_ config.ObjectStorageConfig, meta storage.ObjectRequestMeta) (storage.ObjectShareResponse, storage.HTTPErrorWithCode) {
	f.gotMax = meta.MaxExpiration
	return storage.ObjectShareResponse{URL: "https://s3.example/b/o?sig", ExpiresAt: "2026-11-21T10:15:00Z", ExpiresInSeconds: 900}, storage.HTTPErrorWithCode{}
}

// shareRouter is the real s3-mode router with a fake store behind it.
func shareRouter(t *testing.T, mutate func(*config.Config)) (*Server, *fakeShareStore) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Server.ServeFrontend = false
	if mutate != nil {
		mutate(&cfg)
	}
	store := &fakeShareStore{}
	s := &Server{Config: cfg, logger: zap.NewNop(), store: store}
	s.registerRouter()
	s.registerRoutes()
	return s, store
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return v
}

// TestShareLimitRoute: the limit is a read — routed under both prefixes, and
// still answered on a read-only panel, which refuses the share itself.
func TestShareLimitRoute(t *testing.T) {
	s, _ := shareRouter(t, func(c *config.Config) {
		c.Server.ReadOnly = true
		c.Share.MaxDuration = 45 * time.Minute
	})
	for _, prefix := range []string{"/api", "/s3/api"} {
		rec := serve(s.Router, httptest.NewRequest(http.MethodGet, prefix+"/object/share/limit?bucket=b", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s/object/share/limit on a read-only panel = %d %s, want 200", prefix, rec.Code, rec.Body)
		}
		got := decode[storage.ObjectShareLimitResponse](t, rec)
		if got.MaxSeconds != 45*60 || got.SessionBound {
			t.Errorf("limit = %+v, want max_seconds 2700, session_bound false", got)
		}

		req := httptest.NewRequest(http.MethodPost, prefix+"/object/share?bucket=b&object=o&expiration=1h", nil)
		req.Header.Set(accessKeyHeader, "AK")
		req.Header.Set(secretKeyHeader, "SK")
		if rec := serve(s.Router, req); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s/object/share on a read-only panel = %d, want 403", prefix, rec.Code)
		}
	}

	if rec := serve(s.Router, httptest.NewRequest(http.MethodGet, "/api/object/share/limit", nil)); rec.Code != http.StatusBadRequest {
		t.Errorf("limit without a bucket = %d, want 400", rec.Code)
	}
}

// TestShareLimitDefaultsToUncapped: s3 mode with no share.max_duration offers
// exactly what it always did.
func TestShareLimitDefaultsToUncapped(t *testing.T) {
	s, _ := shareRouter(t, nil)
	rec := serve(s.Router, httptest.NewRequest(http.MethodGet, "/s3/api/object/share/limit?bucket=b", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("limit = %d %s", rec.Code, rec.Body)
	}
	if got := decode[storage.ObjectShareLimitResponse](t, rec); got.MaxSeconds != 0 || got.SessionBound {
		t.Errorf("limit = %+v, want max_seconds 0 (uncapped), session_bound false", got)
	}
}

// TestShareAnswersTheEffectiveExpiry: the share answer carries the store's
// expiry, the configured cap reaches the store, and s3 mode is not session
// bound.
func TestShareAnswersTheEffectiveExpiry(t *testing.T) {
	s, store := shareRouter(t, func(c *config.Config) { c.Share.MaxDuration = 15 * time.Minute })
	req := httptest.NewRequest(http.MethodPost, "/s3/api/object/share?bucket=b&object=o&expiration=1w", nil)
	req.Header.Set(accessKeyHeader, "AK")
	req.Header.Set(secretKeyHeader, "SK")
	rec := serve(s.Router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("share = %d %s", rec.Code, rec.Body)
	}
	if store.gotMax != 15*time.Minute {
		t.Errorf("store was handed cap %v, want share.max_duration 15m", store.gotMax)
	}
	got := decode[storage.ObjectShareResponse](t, rec)
	if got.URL == "" || got.ExpiresAt != "2026-11-21T10:15:00Z" || got.ExpiresInSeconds != 900 || got.SessionBound {
		t.Errorf("share = %+v", got)
	}
}

// TestShareLimitInIAMMode: in iam mode the limit is what remains of the
// injected credential, and links are session bound; a lapsed credential is a
// 409, as for the share itself.
func TestShareLimitInIAMMode(t *testing.T) {
	s := &Server{
		Config: config.Config{
			Server: config.ServerConfig{AuthMode: config.AuthModeIAM},
			Share:  config.ShareConfig{MaxDuration: 2 * time.Hour},
		},
		logger: zap.NewNop(),
	}
	e := echo.New()
	call := func(expiry time.Time) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/object/share/limit?bucket=b", nil), rec)
		c.Set(credentialExpiryKey, expiry)
		if err := s.HandleObjectShareLimit()(c); err != nil {
			t.Fatal(err)
		}
		return rec
	}

	rec := call(time.Now().Add(55 * time.Minute))
	got := decode[storage.ObjectShareLimitResponse](t, rec)
	if got.MaxSeconds > 55*60 || got.MaxSeconds < 55*60-2 || !got.SessionBound {
		t.Errorf("limit = %+v, want about 3300 seconds, session bound", got)
	}

	if rec := call(time.Now().Add(-time.Second)); rec.Code != http.StatusConflict {
		t.Errorf("lapsed credential: limit = %d, want 409", rec.Code)
	}
}
