package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// ConfigResponse tells the SPA how this instance is configured.
//
// It is unauthenticated on purpose: the SPA has to read it before it can know
// whether to render a credential form or a "sign in" button, and it discloses
// nothing beyond which login flow is in use.
type ConfigResponse struct {
	// AuthMode is "s3" or "iam".
	AuthMode string `json:"auth_mode"`
	// LoginURL is where the browser should go to start a login. Empty in s3 mode,
	// where the user types credentials into the panel instead.
	LoginURL string `json:"login_url,omitempty"`
	// Region is the region this instance serves directly.
	Region string `json:"region,omitempty"`
}

// HandleConfig serves GET /config.
//
//	@Summary		Panel configuration
//	@Description	Reports which authentication mode this instance runs, so a client knows whether to present a credential form or an OIDC sign-in.
//	@Tags			Config
//	@Produce		json
//	@Success		200	{object}	ConfigResponse	"Panel configuration"
//	@Router			/api/config [get]
func (s *Server) HandleConfig() echo.HandlerFunc {
	return func(c echo.Context) error {
		resp := ConfigResponse{
			AuthMode: s.Config.Server.AuthMode,
			Region:   s.Config.Server.Region,
		}
		if resp.AuthMode == "" {
			resp.AuthMode = config.AuthModeS3
		}
		if s.Config.Server.IsIAMMode() {
			resp.LoginURL = "/auth/login"
		}
		return c.JSON(http.StatusOK, resp)
	}
}
