package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func get(t *testing.T, h echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := h(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/readyz", nil), rec)); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestLivezIsPlainOK(t *testing.T) {
	rec := get(t, HandleLive)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("livez = %d %q, want 200 ok", rec.Code, rec.Body.String())
	}
}

func up(context.Context) error { return nil }

func down(context.Context) error {
	return errors.New("dial tcp 10.0.0.7:443: connection refused (secret-host.internal)")
}

func TestReadyzRollsUpChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []Check
		status string
		code   int
	}{
		{"all up", []Check{{"iam", true, up}, {"gateway", false, up}}, StatusOK, http.StatusOK},
		{"a non-critical one down", []Check{{"iam", true, up}, {"gateway", false, down}}, StatusDegraded, http.StatusOK},
		{"a critical one down", []Check{{"iam", true, down}, {"gateway", false, up}}, StatusDown, http.StatusServiceUnavailable},
		{"nothing to check", nil, StatusOK, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, NewReadiness(tc.checks, 0, 0).Handle)
			if rec.Code != tc.code {
				t.Errorf("code = %d, want %d", rec.Code, tc.code)
			}
			var report Report
			if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Status != tc.status || len(report.Checks) != len(tc.checks) {
				t.Errorf("report = %+v, want status %s with %d checks", report, tc.status, len(tc.checks))
			}
			for i, c := range report.Checks {
				if c.Name != tc.checks[i].Name {
					t.Errorf("check %d named %q, want %q", i, c.Name, tc.checks[i].Name)
				}
			}
		})
	}
}

// TestReadyzLeaksNothing: anyone who can reach the host can read readyz, so a
// failure says which check failed and nothing about why or where.
func TestReadyzLeaksNothing(t *testing.T) {
	r := NewReadiness([]Check{{"iam", true, down}}, 0, 0)
	var logged string
	r.OnFailure = func(name string, err error) { logged = name + ": " + err.Error() }
	body := get(t, r.Handle).Body.String()
	for _, leak := range []string{"10.0.0.7", "secret-host", "connection refused", "dial"} {
		if strings.Contains(body, leak) {
			t.Errorf("readyz body leaks %q: %s", leak, body)
		}
	}
	if !strings.Contains(logged, "connection refused") {
		t.Errorf("the failure reason was not handed to the log hook: %q", logged)
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(body), &fields)
	if len(fields) != 2 {
		t.Errorf("readyz carries fields beyond status and checks: %s", body)
	}
}

// TestReadyzIsCachedAndShared: a burst of anonymous requests costs one probe
// round, not one per request.
func TestReadyzIsCachedAndShared(t *testing.T) {
	var probes atomic.Int32
	slow := func(context.Context) error {
		probes.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	r := NewReadiness([]Check{{"iam", true, slow}}, time.Minute, 0)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { r.Report(context.Background()) })
	}
	wg.Wait()
	r.Report(context.Background())
	if n := probes.Load(); n != 1 {
		t.Errorf("%d probe rounds for 21 requests inside the TTL, want 1", n)
	}

	// Expired, it probes again.
	r.mu.Lock()
	r.at = time.Now().Add(-2 * time.Minute)
	r.mu.Unlock()
	r.Report(context.Background())
	if n := probes.Load(); n != 2 {
		t.Errorf("%d probe rounds after expiry, want 2", n)
	}
}

func TestReadyzTTLHasAFloor(t *testing.T) {
	if r := NewReadiness(nil, time.Second, 0); r.ttl < 5*time.Second {
		t.Errorf("ttl = %s, want at least 5s", r.ttl)
	}
}

func TestProbeTimesOut(t *testing.T) {
	hang := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	r := NewReadiness([]Check{{"iam", true, hang}}, 0, 20*time.Millisecond)
	start := time.Now()
	if got := r.Report(context.Background()).Status; got != StatusDown {
		t.Errorf("status = %s, want down", got)
	}
	if time.Since(start) > time.Second {
		t.Errorf("a hanging dependency held readyz for %s", time.Since(start))
	}
}

func TestHTTPProbes(t *testing.T) {
	codes := map[string]int{"/ok": 200, "/forbidden": 403, "/redirect": 302, "/broken": 502}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/broken")
		}
		w.WriteHeader(codes[r.URL.Path])
	}))
	defer srv.Close()

	ctx := context.Background()
	for path, wantUp := range map[string]bool{"/ok": true, "/forbidden": true, "/redirect": true, "/broken": false} {
		if err := ReachableProbe(srv.URL + path)(ctx); (err == nil) != wantUp {
			t.Errorf("ReachableProbe %s: err = %v, want up=%v", path, err, wantUp)
		}
	}
	for path, wantUp := range map[string]bool{"/ok": true, "/forbidden": false, "/broken": false} {
		if err := OKProbe(srv.URL + path)(ctx); (err == nil) != wantUp {
			t.Errorf("OKProbe %s: err = %v, want up=%v", path, err, wantUp)
		}
	}
	if err := ReachableProbe("http://127.0.0.1:1")(ctx); err == nil {
		t.Error("ReachableProbe of a closed port reported up")
	}
}
