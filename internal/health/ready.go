package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/singleflight"
)

// States a check, and the whole report, can be in.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDown     = "down"
)

// HandleLive answers GET /livez: the process is up and serving. It checks no
// dependency on purpose, so an outage elsewhere never reads as this process
// being dead.
func HandleLive(c echo.Context) error {
	return c.String(http.StatusOK, "ok")
}

// Check is one dependency readyz reports on.
type Check struct {
	// Name is all a reader learns about the dependency, so it names a role
	// ("iam", "gateway"), never a host.
	Name string
	// Critical marks a dependency without which the panel cannot serve anyone:
	// its failure makes the whole report "down" rather than "degraded".
	Critical bool
	// Probe returns nil when the dependency answers.
	Probe func(ctx context.Context) error
}

// CheckResult is one entry of the readyz body.
type CheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Report is the readyz body.
type Report struct {
	Status string        `json:"status"`
	Checks []CheckResult `json:"checks"`
}

// Readiness answers GET /readyz from its checks.
//
// The answer is public — anyone who can reach the host may ask — so it carries
// check names and states only: no error text, hosts or versions. It is cached
// for TTL, and concurrent askers share one probe round, so anonymous traffic
// cannot drive load onto the dependencies.
type Readiness struct {
	checks  []Check
	ttl     time.Duration
	timeout time.Duration
	// OnFailure, when set, learns why a check failed — for the logs, since the
	// body never says.
	OnFailure func(name string, err error)

	mu     sync.Mutex
	cached Report
	at     time.Time
	group  singleflight.Group
}

// DefaultReadyTTL is how long one readyz answer is reused.
const DefaultReadyTTL = 10 * time.Second

// NewReadiness builds a Readiness over checks. ttl and timeout default to
// DefaultReadyTTL and 2 s; ttl is never below 5 s.
func NewReadiness(checks []Check, ttl, timeout time.Duration) *Readiness {
	if ttl < 5*time.Second {
		ttl = DefaultReadyTTL
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Readiness{checks: checks, ttl: ttl, timeout: timeout}
}

// Handle answers GET /readyz: 200 when ok or degraded, 503 when down.
func (r *Readiness) Handle(c echo.Context) error {
	report := r.Report(c.Request().Context())
	code := http.StatusOK
	if report.Status == StatusDown {
		code = http.StatusServiceUnavailable
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(code, report)
}

// Report returns the cached report, probing again once it is older than the
// TTL.
func (r *Readiness) Report(ctx context.Context) Report {
	r.mu.Lock()
	if !r.at.IsZero() && time.Since(r.at) < r.ttl {
		report := r.cached
		r.mu.Unlock()
		return report
	}
	r.mu.Unlock()

	v, _, _ := r.group.Do("probe", func() (any, error) {
		// Detached from the asker: a client that hangs up must not cancel the
		// round everyone else is waiting on.
		report := r.probe(context.WithoutCancel(ctx))
		r.mu.Lock()
		r.cached, r.at = report, time.Now()
		r.mu.Unlock()
		return report, nil
	})
	return v.(Report)
}

func (r *Readiness) probe(ctx context.Context) Report {
	results := make([]CheckResult, len(r.checks))
	var wg sync.WaitGroup
	for i, check := range r.checks {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()
			status := StatusOK
			if err := check.Probe(ctx); err != nil {
				status = StatusDown
				if r.OnFailure != nil {
					r.OnFailure(check.Name, err)
				}
			}
			results[i] = CheckResult{Name: check.Name, Status: status}
		})
	}
	wg.Wait()

	overall := StatusOK
	for i, result := range results {
		if result.Status != StatusDown {
			continue
		}
		if r.checks[i].Critical {
			overall = StatusDown
			break
		}
		overall = StatusDegraded
	}
	return Report{Status: overall, Checks: results}
}

// probeClient never follows redirects: a 3xx already proves the dependency
// answers, and following it could lead anywhere.
var probeClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// ReachableProbe GETs url and counts any answer below 500 as up. An endpoint
// that refuses an anonymous request (401, 403) is still there to serve the
// signed-in ones.
func ReachableProbe(url string) func(context.Context) error {
	return httpProbe(url, func(code int) bool { return code < http.StatusInternalServerError })
}

// OKProbe GETs url and counts only a 2xx as up, for an endpoint that should
// answer anonymous reads, such as OIDC discovery.
func OKProbe(url string) func(context.Context) error {
	return httpProbe(url, func(code int) bool { return code >= 200 && code < 300 })
}

func httpProbe(url string, up func(int) bool) func(context.Context) error {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		res, err := probeClient.Do(req)
		if err != nil {
			return err
		}
		_ = res.Body.Close()
		if !up(res.StatusCode) {
			return fmt.Errorf("status %d", res.StatusCode)
		}
		return nil
	}
}
