package config

const (
	DefaultLogLevel      = "debug"
	DefaultServerAddress = "127.0.0.1"
	DefaultServerPort    = "8080"
	DefaultDownloadPath  = "/tmp"
	// DefaultCookieName is the session cookie set in AuthModeIAM.
	DefaultCookieName = "s3panel_session"
	// DefaultGroupsClaim is where most OIDC providers put group membership.
	DefaultGroupsClaim = "groups"
)

func DefaultConfig() Config {
	loggerConfig := LoggerConfig{
		Level: DefaultLogLevel,
	}

	serverConfig := ServerConfig{
		Address: DefaultServerAddress,
		Port:    DefaultServerPort,
		// S3-credential login stays the default, so an existing deployment that
		// upgrades in place behaves exactly as before.
		AuthMode:      AuthModeS3,
		DownloadPath:  DefaultDownloadPath,
		ServeFrontend: true,
	}

	// No cross-origin reads. The panel serves its own frontend (and the Vite
	// dev server proxies to it), so its pages are same-origin and need no CORS;
	// an operator lists the origins of a frontend served from elsewhere.
	serverCorsConfig := ServerCorsConfig{}

	oidcConfig := OIDCConfig{
		CookieName:        DefaultCookieName,
		CookieSecure:      true,
		GroupsClaim:       DefaultGroupsClaim,
		PostLoginRedirect: "/",
	}

	return Config{
		Logger:        loggerConfig,
		Server:        serverConfig,
		Cors:          serverCorsConfig,
		ObjectStorage: ObjectStorageConfig{},
		OIDC:          oidcConfig,
		// No cap of its own: a share link lives as long as asked (and, in iam
		// mode, at most as long as the credential that signs it).
		Share: ShareConfig{MaxDuration: 0},
	}
}
