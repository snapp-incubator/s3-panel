package api

import (
	"net/http"
	"strings"

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
	// LoginURL is where the browser should go to start a login, base path
	// included. Empty in s3 mode, where the user types credentials into the
	// panel instead.
	LoginURL string `json:"login_url,omitempty"`
	// Region is the region this instance serves directly.
	Region string `json:"region,omitempty"`
	// ReadOnly reports that every mutating operation is refused, so the UI can
	// hide the controls rather than offer actions that will 403.
	ReadOnly bool `json:"read_only"`
	// IAMConsoleURL is where bucket cards link to manage access. Only reported
	// in iam mode, and empty when the deployment has not configured it.
	IAMConsoleURL string `json:"iam_console_url,omitempty"`
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
			ReadOnly: s.Config.Server.IsReadOnly(),
		}
		if resp.AuthMode == "" {
			resp.AuthMode = config.AuthModeS3
		}
		if s.Config.Server.IsIAMMode() {
			resp.LoginURL = s.Config.Server.Base() + "/auth/login"
			resp.IAMConsoleURL = strings.TrimRight(s.Config.Server.IAMConsoleURL, "/")
		}
		return c.JSON(http.StatusOK, resp)
	}
}
