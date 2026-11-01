package api

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
	"github.com/snapp-incubator/S3-Panel/internal/version"
)

// How long the console's answer to "may this person view the app" is trusted
// (proposal 015 §4). Within viewFreshFor it is reused as is. After that the
// console is asked again; if it cannot answer, a YES no older than
// viewStaleFor still stands, to ride out a short restart, and after that the
// panel refuses.
const (
	viewFreshFor = 60 * time.Second
	viewStaleFor = 10 * time.Minute
)

// viewVerdict is what the gate decided.
type viewVerdict int

const (
	viewAllowed viewVerdict = iota
	// viewDenied: the console said this person may not view the app.
	viewDenied
	// viewUnknown: the console could not be asked, and no recent yes covers
	// the gap. Refused all the same: failing open would readmit revoked people
	// whenever the console is down.
	viewUnknown
)

// viewGate caches the console's viewer answers per person (OIDC subject).
type viewGate struct {
	ask     func(ctx context.Context, subject string) (bool, error)
	now     func() time.Time
	onError func(subject string, err error)

	mu      sync.Mutex
	answers map[string]viewAnswer
	group   singleflight.Group
}

type viewAnswer struct {
	view bool
	at   time.Time
}

// maxViewAnswers bounds the cache; past it, answers too old to use are swept.
const maxViewAnswers = 10000

func newViewGate(ask func(ctx context.Context, subject string) (bool, error)) *viewGate {
	return &viewGate{ask: ask, now: time.Now, answers: map[string]viewAnswer{}}
}

// check answers from the cache while it is fresh, asking the console otherwise.
func (g *viewGate) check(ctx context.Context, subject string) viewVerdict {
	if subject == "" {
		return viewDenied
	}
	g.mu.Lock()
	prior, known := g.answers[subject]
	g.mu.Unlock()
	if known && g.now().Sub(prior.at) < viewFreshFor {
		return verdictOf(prior.view)
	}
	return g.askConsole(ctx, subject, prior, known)
}

// refresh asks the console now, whatever the cache holds. Sign-in uses it, so
// a session always starts from a current answer.
func (g *viewGate) refresh(ctx context.Context, subject string) viewVerdict {
	if subject == "" {
		return viewDenied
	}
	g.mu.Lock()
	prior, known := g.answers[subject]
	g.mu.Unlock()
	return g.askConsole(ctx, subject, prior, known)
}

func (g *viewGate) askConsole(ctx context.Context, subject string, prior viewAnswer, known bool) viewVerdict {
	// One question per person at a time: the SPA fires several calls at once,
	// and they all arrive here together when the answer goes stale.
	v, err, _ := g.group.Do(subject, func() (any, error) {
		// Detached from the asker: a request that ends must not cancel the
		// answer the others are waiting on. The client's own timeout bounds it.
		view, err := g.ask(context.WithoutCancel(ctx), subject)
		if err == nil {
			g.store(subject, view)
		}
		return view, err
	})
	if err == nil {
		return verdictOf(v.(bool))
	}
	if g.onError != nil {
		g.onError(subject, err)
	}
	if known && prior.view && g.now().Sub(prior.at) < viewStaleFor {
		return viewAllowed
	}
	return viewUnknown
}

func (g *viewGate) store(subject string, view bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if len(g.answers) >= maxViewAnswers {
		for s, a := range g.answers {
			if now.Sub(a.at) >= viewStaleFor {
				delete(g.answers, s)
			}
		}
	}
	g.answers[subject] = viewAnswer{view: view, at: now}
}

func verdictOf(view bool) viewVerdict {
	if view {
		return viewAllowed
	}
	return viewDenied
}

// panelViewDenied is the reason an API refusal carries, so the SPA can tell
// "you lost access to this app" from an ordinary 403 and reload into the
// "No access" page.
const panelViewDenied = "panel_view_denied"

// requireView refuses API calls from people the console says may not view this
// app (a no-op unless the panel integration is on). It runs after the session
// guard, which established who is asking.
func (s *Server) requireView() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		if s.view == nil {
			return next
		}
		return func(c echo.Context) error {
			session, _ := auth.SessionFrom(c)
			if s.view.check(c.Request().Context(), session.Subject) != viewAllowed {
				return c.JSON(http.StatusForbidden, map[string]string{
					"message": "you do not have access to this app",
					"reason":  panelViewDenied,
				})
			}
			return next(c)
		}
	}
}

// registerPanel joins the console when panel.enabled is on: it checks the
// settings, builds the registry client and the view gate, and starts
// registering the manifest in the background.
//
// It refuses to start on a setting the console would reject anyway, so the
// mistake surfaces at deploy time instead of as a registration that fails
// forever in the logs.
func (s *Server) registerPanel(ctx context.Context) error {
	if !s.Config.Panel.Enabled {
		return nil
	}
	s.Config.Panel = s.Config.Panel.WithDefaults()
	if !s.Config.Server.IsIAMMode() || s.auth == nil {
		return fmt.Errorf("panel.enabled requires server.auth_mode %q: the console is asked about people by their OIDC subject", config.AuthModeIAM)
	}
	manifest := s.panelManifest()
	for _, p := range manifestPaths(manifest) {
		if err := s.checkPanelPath(p); err != nil {
			return err
		}
	}

	apiURL := strings.TrimSpace(s.Config.Panel.APIURL)
	if apiURL == "" {
		apiURL = s.control.APIURL()
	}
	panel, err := control.NewPanel(apiURL, s.Config.Panel.APIKey, 0)
	if err != nil {
		return err
	}

	s.view = newViewGate(panel.CanView)
	s.view.onError = func(subject string, err error) {
		s.logger.Warn("could not ask the console whether the person may view this app",
			zap.String("subject", subject), zap.Error(err))
	}
	// At sign-in the answer is fetched fresh, not taken from the cache.
	s.auth.OnSignIn = func(ctx context.Context, session auth.Session) {
		s.view.refresh(ctx, session.Subject)
	}
	s.logger.Info("### Panel integration on ###",
		zap.String("app", s.Config.Panel.AppKey), zap.String("api", apiURL))

	go s.keepRegistered(ctx, panel, manifest)
	return nil
}

// panelManifest is what this app registers: its description and version,
// where its pages and health endpoints are, and how to open catalog resources
// here.
func (s *Server) panelManifest() control.PanelManifest {
	base := s.Config.Server.Base()
	cfg := s.Config.Panel.WithDefaults()

	nav := make([]control.PanelNavItem, 0, len(cfg.Nav))
	for _, n := range cfg.Nav {
		nav = append(nav, control.PanelNavItem{Label: n.Label, Path: n.Path})
	}
	if len(nav) == 0 {
		nav = append(nav, control.PanelNavItem{Label: "Buckets", Path: base + "/object-storage/s3-bucket/buckets"})
	}
	links := make([]control.PanelLink, 0, len(cfg.Links))
	for _, l := range cfg.Links {
		links = append(links, control.PanelLink{Resource: l.Resource, Path: l.Path})
	}
	return control.PanelManifest{
		Description: cfg.Description,
		Version:     version.Get(),
		Nav:         nav,
		Links:       links,
		Health:      control.PanelHealth{Live: base + "/livez", Ready: base + "/readyz"},
	}
}

func manifestPaths(m control.PanelManifest) []string {
	paths := []string{m.Health.Live, m.Health.Ready}
	for _, n := range m.Nav {
		paths = append(paths, n.Path)
	}
	for _, l := range m.Links {
		paths = append(paths, l.Path)
	}
	return paths
}

// checkPanelPath applies the console's own rule to a manifest path: a relative
// path under this app's base path, with no scheme, host, ".." or "//".
func (s *Server) checkPanelPath(p string) error {
	base := s.Config.Server.Base()
	pathPart, _, _ := strings.Cut(p, "?")
	switch {
	case !strings.HasPrefix(p, "/") || strings.Contains(pathPart, "//") || strings.Contains(p, `\`):
		return fmt.Errorf("panel: manifest path %q must be a relative path starting with one \"/\"", p)
	case strings.Contains(pathPart, ".."):
		return fmt.Errorf("panel: manifest path %q must not contain \"..\"", p)
	case base != "" && pathPart != base && !strings.HasPrefix(pathPart, base+"/"):
		return fmt.Errorf("panel: manifest path %q is outside server.base_path %q", p, base)
	}
	return nil
}

// keepRegistered registers the manifest now, and again every RegisterInterval
// once it has succeeded, so the console sees a live registration. A failure is
// retried with backoff up to five minutes; it never stops the panel serving.
func (s *Server) keepRegistered(ctx context.Context, panel *control.Panel, manifest control.PanelManifest) {
	const firstRetry, maxRetry = 5 * time.Second, 5 * time.Minute
	retry := firstRetry
	for {
		wait := s.Config.Panel.RegisterInterval
		app, err := panel.Register(ctx, manifest)
		if err != nil {
			s.logger.Error("could not register with the console; retrying",
				zap.Duration("in", retry), zap.Error(err))
			wait, retry = retry, min(retry*2, maxRetry)
		} else {
			s.logger.Info("registered with the console",
				zap.String("app", app.Key), zap.String("state", app.State), zap.String("version", manifest.Version))
			retry = firstRetry
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// renderPage answers a request for one of the SPA's pages. With the panel
// integration on, a signed-in person the console will not admit gets the
// "No access" page instead; a visitor who is not signed in gets the SPA, which
// offers the sign-in.
func (s *Server) renderPage(c echo.Context, index []byte) error {
	if s.view != nil && s.auth != nil {
		if session, ok := s.auth.Load(c); ok {
			if verdict := s.view.check(c.Request().Context(), session.Subject); verdict != viewAllowed {
				return s.renderNoAccess(c, verdict)
			}
		}
	}
	return serveIndex(c, index)
}

var noAccessPage = template.Must(template.New("no-access").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body{margin:0;font-family:system-ui,-apple-system,"Segoe UI",sans-serif;color:#1f2328;background:#f6f8fa}
main{max-width:34rem;margin:18vh auto 0;padding:0 1rem}
h1{font-size:1.5rem;font-weight:600;margin:0 0 .75rem}
p{line-height:1.5;margin:0 0 1rem;color:#59636e}
a{color:#0969da}
@media (prefers-color-scheme:dark){body{color:#e6edf3;background:#0d1117}p{color:#9198a1}a{color:#4493f8}}
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
<p><a href="{{.HomeURL}}">Back to all apps</a> &middot; <a href="{{.SignOutURL}}">Sign out</a></p>
</main>
</body>
</html>
`))

// renderNoAccess is the page a refused person sees. It is the panel's own
// minimal page rather than the SPA, so a refused person never loads the app.
func (s *Server) renderNoAccess(c echo.Context, verdict viewVerdict) error {
	data := struct {
		Title, Message, HomeURL, SignOutURL string
	}{
		Title:      "No access",
		Message:    "You do not have access to this app. Ask its owners for access, or open another app.",
		HomeURL:    s.Config.Panel.HomeURL,
		SignOutURL: s.Config.Panel.SignOutURL,
	}
	if verdict == viewUnknown {
		data.Message = "Your access to this app cannot be checked right now. Try again in a few minutes."
	}
	var buf bytes.Buffer
	if err := noAccessPage.Execute(&buf, data); err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.HTMLBlob(http.StatusForbidden, buf.Bytes())
}
