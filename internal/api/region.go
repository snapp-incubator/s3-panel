package api

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// buildRegionTargets parses the configured remote region endpoints into URLs,
// skipping the local region and empty URLs. Called once at startup.
func (s *Server) buildRegionTargets() error {
	s.regionTargets = make(map[string]*url.URL)

	for name, raw := range s.Config.Server.RegionEndpoints {
		if name == s.Config.Server.Region || raw == "" {
			continue
		}

		target, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid endpoint %q for region %q: %w", raw, name, err)
		}

		s.regionTargets[name] = target
	}

	return nil
}

// regionRouter reverse-proxies an API request to the backend of the region
// named in the "region" header. Requests for the local region (or with no
// region header) are handled in-process by the next handler.
//
// In iam mode nothing is proxied: the control endpoint answers for every
// region, and the credential minted for a bucket names the gateway of the
// region it lives in, so this instance serves all of them. The header then only
// says which region a bucket name refers to, which requireBucketPermission
// reads.
func (s *Server) regionRouter() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if s.Config.Server.IsIAMMode() {
				return next(c)
			}

			region := c.Request().Header.Get("region")
			if region == "" || region == s.Config.Server.Region {
				return next(c)
			}

			target, ok := s.regionTargets[region]
			if !ok {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					fmt.Sprintf("unknown region %q", region),
				)
			}

			// SetURL routes to the target's scheme/host, joins the paths, and
			// rewrites the outbound Host header so the remote ingress matches.
			proxy := &httputil.ReverseProxy{
				Rewrite: func(r *httputil.ProxyRequest) {
					r.SetURL(target)
				},
			}

			s.logger.Info("proxying request to region",
				zap.String("region", region),
				zap.String("target", target.String()),
			)
			proxy.ServeHTTP(c.Response(), c.Request())

			return nil
		}
	}
}

// regions returns the region names this instance can serve, local first.
func (s *Server) regions() []string {
	remotes := make([]string, 0, len(s.regionTargets))
	for name := range s.regionTargets {
		remotes = append(remotes, name)
	}
	sort.Strings(remotes)

	regions := make([]string, 0, len(remotes)+1)
	if s.Config.Server.Region != "" {
		regions = append(regions, s.Config.Server.Region)
	}

	return append(regions, remotes...)
}

// HandleRegions lists the regions selectable from this instance.
//
//	@Summary	List selectable regions
//	@Tags		region
//	@Produce	json
//	@Success	200	{object}	map[string]interface{}
//	@Router		/regions [get]
func (s *Server) HandleRegions() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"current": s.Config.Server.Region,
			"regions": s.regions(),
		})
	}
}
