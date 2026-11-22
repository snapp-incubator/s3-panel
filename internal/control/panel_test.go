package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeRegistry answers the panel API as the contract shapes it (proposal 018
// §1.4), recording what it was asked.
func fakeRegistry(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer app_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"unknown app key"}`))
			return
		}
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRegisterPutsTheManifest(t *testing.T) {
	var got PanelManifest
	var method, path, contentType string
	srv := fakeRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"key":"storage","name":"Object Storage","prefix":"/storage/","state":"pending","status":"unknown"}`))
	})
	p, err := NewPanel(srv.URL+"/api/", "app_test", 0)
	if err != nil {
		t.Fatal(err)
	}
	app, err := p.Register(context.Background(), PanelManifest{
		Description: "Buckets", Version: "1.9.0",
		Nav:    []PanelNavItem{{Label: "Buckets", Path: "/storage/object-storage/s3-bucket/buckets"}},
		Links:  []PanelLink{{Resource: "ceph-*|bucket", Path: "/storage/x/{name}"}},
		Health: PanelHealth{Live: "/storage/livez", Ready: "/storage/readyz"},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if method != http.MethodPut || path != "/api/panel/apps/self" || contentType != "application/json" {
		t.Errorf("sent %s %s (%s), want PUT /api/panel/apps/self as JSON", method, path, contentType)
	}
	if got.Health.Ready != "/storage/readyz" || len(got.Nav) != 1 || got.Links[0].Resource != "ceph-*|bucket" {
		t.Errorf("manifest arrived as %+v", got)
	}
	if app.Key != "storage" || app.State != "pending" {
		t.Errorf("app = %+v", app)
	}
}

func TestRegisterReportsRefusals(t *testing.T) {
	srv := fakeRegistry(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"nav path outside the app's prefix"}`))
	})
	p, _ := NewPanel(srv.URL+"/api", "app_test", 0)
	_, err := p.Register(context.Background(), PanelManifest{})
	var ce *Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadRequest || ce.Message != "nav path outside the app's prefix" {
		t.Errorf("err = %v, want the console's 400 and message", err)
	}
}

func TestCanView(t *testing.T) {
	var gotPath string
	answers := map[string]string{
		"u-alice": `{"view":true}`,
		"u-bob":   `{"view":false}`,
		"u-odd":   `{}`,
	}
	srv := fakeRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		sub := r.URL.Path[len("/api/panel/apps/self/viewers/"):]
		if sub == "u-limited" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(answers[sub]))
	})
	p, _ := NewPanel(srv.URL+"/api", "app_test", 0)
	ctx := context.Background()

	if ok, err := p.CanView(ctx, "u-alice"); err != nil || !ok {
		t.Errorf("alice: %v %v, want true", ok, err)
	}
	if ok, err := p.CanView(ctx, "u-bob"); err != nil || ok {
		t.Errorf("bob: %v %v, want false", ok, err)
	}
	if _, err := p.CanView(ctx, "u-odd"); err == nil {
		t.Error("a body without view was read as an answer")
	}
	var ce *Error
	if _, err := p.CanView(ctx, "u-limited"); !errors.As(err, &ce) || ce.Status != http.StatusTooManyRequests {
		t.Errorf("rate limited: err = %v, want a 429 *Error", err)
	}
	if _, err := p.CanView(ctx, ""); err == nil {
		t.Error("asked about an empty subject")
	}

	// The subject is one path segment, however odd it is.
	_, _ = p.CanView(ctx, "a/../b?c")
	if gotPath != "/api/panel/apps/self/viewers/a%2F..%2Fb%3Fc" {
		t.Errorf("subject sent as %q, want it escaped into one segment", gotPath)
	}

	wrong, _ := NewPanel(srv.URL+"/api", "app_other", 0)
	if _, err := wrong.CanView(ctx, "u-alice"); !errors.As(err, &ce) || ce.Status != http.StatusUnauthorized {
		t.Errorf("wrong key: err = %v, want a 401 *Error", err)
	}
}

func TestNewPanelNeedsAKeyAndURL(t *testing.T) {
	if _, err := NewPanel("", "app_x", 0); err == nil {
		t.Error("accepted an empty API URL")
	}
	if _, err := NewPanel("https://iam.example.com/api", " ", 0); err == nil {
		t.Error("accepted an empty key")
	}
}

// TestViewerReadsOwnerTeamAndReason: the contract's later fields are read when
// present and left empty when an older console does not send them.
func TestViewerReadsOwnerTeamAndReason(t *testing.T) {
	answers := map[string]string{
		"u-new": `{"view":false,"owner_team":" storage-team ","reason":"none"}`,
		"u-old": `{"view":true}`,
		"u-odd": `{"view":true,"reason":"because"}`,
	}
	srv := fakeRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(answers[r.URL.Path[len("/api/panel/apps/self/viewers/"):]]))
	})
	p, _ := NewPanel(srv.URL+"/api", "app_test", 0)
	ctx := context.Background()
	for sub, want := range map[string]Viewer{
		"u-new": {View: false, OwnerTeam: "storage-team", Reason: ViewerNone},
		"u-old": {View: true},
		"u-odd": {View: true, Reason: "because"},
	} {
		if got, err := p.Viewer(ctx, sub); err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", sub, got, err, want)
		}
	}
}
