package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// console is a fake of the console's view answers, with a clock the test
// moves.
type console struct {
	mu    sync.Mutex
	view  map[string]bool
	down  bool
	calls atomic.Int32
}

func (f *console) ask(_ context.Context, subject string) (bool, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false, errors.New("console unreachable")
	}
	return f.view[subject], nil
}

func (f *console) set(subject string, view bool) {
	f.mu.Lock()
	f.view[subject] = view
	f.mu.Unlock()
}

func (f *console) setDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newGate(f *console) (*viewGate, *clock) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	g := newViewGate(f.ask)
	g.now = c.now
	return g, c
}

// TestViewAnswersAreFreshForAMinute: revoking view takes effect within 60 s,
// and inside those 60 s the console is not asked again.
func TestViewAnswersAreFreshForAMinute(t *testing.T) {
	f := &console{view: map[string]bool{"u-alice": true}}
	g, clk := newGate(f)
	ctx := context.Background()

	if g.check(ctx, "u-alice") != viewAllowed {
		t.Fatal("a viewer was refused")
	}
	f.set("u-alice", false)
	clk.advance(59 * time.Second)
	if g.check(ctx, "u-alice") != viewAllowed || f.calls.Load() != 1 {
		t.Errorf("inside the fresh window: %d calls, want 1 and the cached yes", f.calls.Load())
	}
	clk.advance(2 * time.Second)
	if g.check(ctx, "u-alice") != viewDenied {
		t.Error("a revoked viewer was still admitted after 60 s")
	}
	if f.calls.Load() != 2 {
		t.Errorf("%d calls, want 2", f.calls.Load())
	}
}

// TestLastKnownGoodRidesOutAnOutage, for up to ten minutes and no longer.
func TestLastKnownGoodRidesOutAnOutage(t *testing.T) {
	f := &console{view: map[string]bool{"u-alice": true}}
	g, clk := newGate(f)
	ctx := context.Background()
	var errs atomic.Int32
	g.onError = func(string, error) { errs.Add(1) }

	g.check(ctx, "u-alice")
	f.setDown(true)

	clk.advance(5 * time.Minute)
	if g.check(ctx, "u-alice") != viewAllowed {
		t.Error("a viewer was refused 5 min into an outage, inside the last-known-good window")
	}
	clk.advance(5*time.Minute + time.Second)
	if g.check(ctx, "u-alice") != viewUnknown {
		t.Error("a viewer was admitted more than 10 min after the last answer: the gate failed open")
	}
	if errs.Load() != 2 {
		t.Errorf("onError ran %d times, want 2", errs.Load())
	}

	// Back up: the next check asks again and admits.
	f.setDown(false)
	if g.check(ctx, "u-alice") != viewAllowed {
		t.Error("not readmitted once the console answered again")
	}
}

// TestNoAnswerFailsClosed: with nothing to fall back on, or a last answer of
// "no", an outage refuses.
func TestNoAnswerFailsClosed(t *testing.T) {
	f := &console{view: map[string]bool{"u-bob": false}, down: true}
	g, clk := newGate(f)
	ctx := context.Background()
	if g.check(ctx, "u-alice") != viewUnknown {
		t.Error("a never-checked person was admitted while the console was down")
	}

	f.setDown(false)
	g.check(ctx, "u-bob")
	f.setDown(true)
	clk.advance(2 * time.Minute)
	if g.check(ctx, "u-bob") == viewAllowed {
		t.Error("a last answer of no turned into yes during an outage")
	}
	if g.check(ctx, "") != viewDenied {
		t.Error("a request with no subject was admitted")
	}
}

// TestConcurrentChecksAskOnce: the SPA fires several calls together, and they
// arrive together when the answer goes stale.
func TestConcurrentChecksAskOnce(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	g := newViewGate(func(context.Context, string) (bool, error) {
		calls.Add(1)
		<-release
		return true, nil
	})
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { g.check(context.Background(), "u-alice") })
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("%d console calls for 10 concurrent checks, want 1", n)
	}
}

func TestRefreshIgnoresTheCache(t *testing.T) {
	f := &console{view: map[string]bool{"u-alice": true}}
	g, _ := newGate(f)
	g.check(context.Background(), "u-alice")
	f.set("u-alice", false)
	if g.refresh(context.Background(), "u-alice") != viewDenied || f.calls.Load() != 2 {
		t.Error("refresh answered from the cache")
	}
}

// TestRequireViewRefusesAPICalls with a 403 the SPA can recognise.
func TestRequireViewRefusesAPICalls(t *testing.T) {
	f := &console{view: map[string]bool{"u-alice": true, "u-bob": false}}
	s := &Server{view: newViewGate(f.ask)}
	h := s.requireView()(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	for subject, want := range map[string]int{"u-alice": http.StatusNoContent, "u-bob": http.StatusForbidden} {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/storage/s3/api/bucket/list", nil), rec)
		c.Set(auth.SessionContextKey, auth.Session{Subject: subject})
		if err := h(c); err != nil {
			t.Fatal(err)
		}
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", subject, rec.Code, want)
		}
		if want == http.StatusForbidden && !strings.Contains(rec.Body.String(), panelViewDenied) {
			t.Errorf("%s: refusal %q does not carry the reason", subject, rec.Body.String())
		}
	}

	// Off, it is a no-op.
	off := (&Server{}).requireView()(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	if err := off(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)); err != nil || rec.Code != http.StatusNoContent {
		t.Errorf("gate off: %d %v", rec.Code, err)
	}
}

// signedInRequest returns a GET carrying a real session cookie for subject.
func signedInRequest(t *testing.T, a *auth.Authenticator, path, subject string) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	session := auth.Session{
		Subject: subject, AccessToken: "tok",
		ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour),
	}
	if err := a.Save(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), session); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

// TestPagesForRefusedPeopleAreNoAccess: a signed-in person the console will
// not admit never loads the app; a viewer and an anonymous visitor (who must
// see the sign-in) get the SPA.
func TestPagesForRefusedPeopleAreNoAccess(t *testing.T) {
	s, _ := actAsServer(t)
	s.Config.Panel = config.PanelConfig{Enabled: true}.WithDefaults()
	f := &console{view: map[string]bool{"u-alice": true, "u-bob": false}}
	s.view = newViewGate(f.ask)

	e := echo.New()
	e.Use(securityHeaders())
	e.Use(spaMiddleware(builtFrontend, "", s.renderPage))

	alice := serve(e, signedInRequest(t, s.auth, "/object-storage/s3-bucket", "u-alice"))
	if alice.Code != http.StatusOK || !strings.Contains(alice.Body.String(), `id="root"`) {
		t.Errorf("viewer: %d, want the SPA", alice.Code)
	}

	bob := serve(e, signedInRequest(t, s.auth, "/object-storage/s3-bucket", "u-bob"))
	if bob.Code != http.StatusForbidden || !strings.Contains(bob.Body.String(), "No access") || strings.Contains(bob.Body.String(), `id="root"`) {
		t.Errorf("non-viewer: %d %q, want the No access page", bob.Code, bob.Body.String())
	}
	for _, link := range []string{`href="/"`, `href="/signout"`} {
		if !strings.Contains(bob.Body.String(), link) {
			t.Errorf("No access page lacks %s", link)
		}
	}

	f.setDown(true)
	carol := serve(e, signedInRequest(t, s.auth, "/", "u-carol"))
	if carol.Code != http.StatusForbidden || !strings.Contains(carol.Body.String(), "cannot be checked") {
		t.Errorf("console down, never checked: %d %q, want a fail-closed No access page", carol.Code, carol.Body.String())
	}

	anon := serve(e, httptest.NewRequest(http.MethodGet, "/object-storage/s3-bucket", nil))
	if anon.Code != http.StatusOK || !strings.Contains(anon.Body.String(), `id="root"`) {
		t.Errorf("anonymous: %d, want the SPA and its sign-in", anon.Code)
	}
}

// registryFake is the console's registry as the contract shapes it.
func registryFake(t *testing.T) (*httptest.Server, chan control.PanelManifest) {
	t.Helper()
	got := make(chan control.PanelManifest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer app_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/panel/apps/self":
			body, _ := io.ReadAll(r.Body)
			var m control.PanelManifest
			_ = json.Unmarshal(body, &m)
			got <- m
			_, _ = w.Write([]byte(`{"key":"storage","state":"pending"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/panel/apps/self/viewers/"):
			_, _ = w.Write([]byte(`{"view":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// TestRegisterPanelRegistersTheManifest end to end against the fake registry.
func TestRegisterPanelRegistersTheManifest(t *testing.T) {
	registry, got := registryFake(t)
	s, _ := actAsServer(t)
	s.Config.Server.BasePath = "/storage"
	s.Config.Panel = config.PanelConfig{
		Enabled: true, APIURL: registry.URL + "/api", APIKey: "app_test",
		Links: []config.PanelLink{{Resource: "ceph-*|bucket", Path: "/storage/iam/buckets/{name}?region={region}"}},
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := s.registerPanel(ctx); err != nil {
		t.Fatalf("registerPanel: %v", err)
	}
	select {
	case m := <-got:
		if m.Health.Live != "/storage/livez" || m.Health.Ready != "/storage/readyz" {
			t.Errorf("health = %+v", m.Health)
		}
		if len(m.Nav) != 1 || m.Nav[0].Path != "/storage/object-storage/s3-bucket/buckets" {
			t.Errorf("default nav = %+v", m.Nav)
		}
		if len(m.Links) != 1 || m.Links[0].Resource != "ceph-*|bucket" || m.Description == "" || m.Version == "" {
			t.Errorf("manifest = %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manifest was never registered")
	}
	if s.view == nil || s.view.check(ctx, "u-alice") != viewAllowed {
		t.Error("the view gate does not ask the registry")
	}
	if s.auth.OnSignIn == nil {
		t.Error("sign-in does not refresh the view answer")
	}
}

func TestRegisterPanelRefusesBadSettings(t *testing.T) {
	registry, _ := registryFake(t)
	for name, mutate := range map[string]func(s *Server){
		"s3 mode": func(s *Server) { s.Config.Server.AuthMode = config.AuthModeS3 },
		"no key":  func(s *Server) { s.Config.Panel.APIKey = "" },
		"nav outside the base path": func(s *Server) {
			s.Config.Panel.Nav = []config.PanelNavItem{{Label: "IAM", Path: "/iam/"}}
		},
		"link with ..": func(s *Server) {
			s.Config.Panel.Links = []config.PanelLink{{Resource: "ceph-*|bucket", Path: "/storage/../iam/{name}"}}
		},
		"protocol-relative nav": func(s *Server) {
			s.Config.Panel.Nav = []config.PanelNavItem{{Label: "x", Path: "//evil.example.com/storage/"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := actAsServer(t)
			s.Config.Server.BasePath = "/storage"
			s.Config.Panel = config.PanelConfig{Enabled: true, APIURL: registry.URL + "/api", APIKey: "app_test"}
			mutate(s)
			if err := s.registerPanel(t.Context()); err == nil {
				t.Error("registerPanel accepted it")
			}
		})
	}
}

// TestPanelOffChangesNothing: the standalone default registers nothing,
// gates nothing and reports no panel to the SPA.
func TestPanelOffChangesNothing(t *testing.T) {
	s, _ := actAsServer(t)
	if err := s.registerPanel(t.Context()); err != nil || s.view != nil || s.auth.OnSignIn != nil {
		t.Errorf("panel off: err %v, gate %v", err, s.view)
	}
	e := echo.New()
	e.GET("/api/config", s.HandleConfig())
	if body := serve(e, httptest.NewRequest(http.MethodGet, "/api/config", nil)).Body.String(); strings.Contains(body, "panel") {
		t.Errorf("/api/config mentions the panel while it is off: %s", body)
	}
}

// TestPagesEmbedTheConsoleHeader (proposal 015 §5): with the panel on, the
// header bundle loads from the console's path under this response's nonce, and
// its element comes first in <body>. Off, the page is the build's own.
func TestPagesEmbedTheConsoleHeader(t *testing.T) {
	s, _ := actAsServer(t)
	s.Config.Panel = config.PanelConfig{Enabled: true}.WithDefaults()
	s.view = newViewGate((&console{view: map[string]bool{}}).ask)

	e := echo.New()
	e.Use(securityHeaders())
	e.Use(spaMiddleware(builtFrontend, "", s.renderPage))
	rec := serve(e, httptest.NewRequest(http.MethodGet, "/object-storage/s3-bucket", nil))
	body := rec.Body.String()

	csp := rec.Header().Get("Content-Security-Policy-Report-Only")
	_, after, _ := strings.Cut(csp, "'nonce-")
	nonce, _, _ := strings.Cut(after, "'")
	if !strings.Contains(body, `<script src="/panel/header/v1/header.js" nonce="`+nonce+`" data-panel-header defer></script>`) {
		t.Errorf("header bundle missing or without the response nonce:\n%s", body)
	}
	if strings.Count(body, `nonce="`+nonce+`"`) != 3 {
		t.Errorf("want the app's script, the event recorder and the header bundle under the nonce:\n%s", body)
	}
	if !strings.Contains(body, `<body><snappcloud-header app="storage"></snappcloud-header><div id="root">`) {
		t.Errorf("header element is not first in <body>:\n%s", body)
	}
	if strings.Index(body, "header.js") > strings.Index(body, "</head>") {
		t.Errorf("header bundle is outside <head>")
	}

	// Off: the page is untouched.
	s.view = nil
	plain := serve(e, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(plain.Body.String(), "snappcloud-header") || strings.Contains(plain.Body.String(), "__panelHeader") {
		t.Errorf("panel off, page still embeds the header:\n%s", plain.Body.String())
	}
}

func TestNoAccessPageKeepsTheHeader(t *testing.T) {
	s, _ := actAsServer(t)
	s.Config.Panel = config.PanelConfig{Enabled: true, AppKey: "storage"}.WithDefaults()
	s.view = newViewGate((&console{view: map[string]bool{"u-bob": false}}).ask)

	e := echo.New()
	e.Use(securityHeaders())
	e.Use(spaMiddleware(builtFrontend, "", s.renderPage))
	rec := serve(e, signedInRequest(t, s.auth, "/", "u-bob"))
	body := rec.Body.String()
	if rec.Code != http.StatusForbidden || !strings.Contains(body, `<snappcloud-header app="storage">`) || !strings.Contains(body, `src="/panel/header/v1/header.js" nonce="`) {
		t.Errorf("No access page without the console header (%d):\n%s", rec.Code, body)
	}
	if strings.Contains(body, cspNoncePlaceholder) || strings.Contains(body, `nonce=""`) {
		t.Errorf("No access page carries no real nonce:\n%s", body)
	}
}

func TestWithPanelHeaderLeavesOddPagesAlone(t *testing.T) {
	s := &Server{Config: config.Config{Panel: config.PanelConfig{Enabled: true}}}
	for _, page := range []string{"<p>no head or body</p>", "<html><body></body><head></head></html>"} {
		if got := string(s.withPanelHeader([]byte(page))); got != page {
			t.Errorf("rewrote %q into %q", page, got)
		}
	}
	got := string(s.withPanelHeader([]byte(`<html><head></head><BODY class="x"><div></div></BODY></html>`)))
	if !strings.Contains(got, `<BODY class="x"><snappcloud-header`) {
		t.Errorf("did not place the element after a body tag with attributes: %s", got)
	}
}
