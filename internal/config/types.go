package config

import "time"

// Authentication modes.
const (
	// AuthModeS3 is the default: the user supplies their own S3 access key and
	// secret key, and every request is signed with them. The gateway is the only
	// authorization layer, so a user sees exactly the buckets their own RGW user
	// owns.
	AuthModeS3 = "s3"
	// AuthModeIAM authenticates the user with OIDC and asks an external,
	// S3-compatible control endpoint which buckets they may use and with what
	// permission. Object credentials come from that endpoint's STS surface.
	//
	// Nothing here is specific to a particular authorization service: the panel
	// speaks only OIDC, S3 and STS.
	AuthModeIAM = "iam"
)

type LoggerConfig struct {
	Level string `json:"level" koanf:"level"`
}

type ServerConfig struct {
	Address string `json:"address"         koanf:"address"`
	Port    string `json:"port"            koanf:"port"`
	// AuthMode selects how a caller is identified: AuthModeS3 (default) or
	// AuthModeIAM. The three settings below apply to AuthModeS3 only — they are a
	// single static bearer token in front of the whole API, not a user identity.
	AuthMode      string `json:"auth_mode"       koanf:"auth_mode"`
	AuthEnabled   string `json:"auth_enabled"    koanf:"auth_enabled"`
	AuthKeyLookup string `json:"auth_key_lookup" koanf:"auth_key_lookup"`
	AuthToken     string `json:"auth_token"      koanf:"auth_token"`
	DownloadPath  string `json:"download_path"   koanf:"download_path"`
	// ReadOnly refuses every operation that would modify the storage backend —
	// uploads, deletes, bucket creation and share links — while leaving browsing
	// and downloads intact.
	//
	// It exists for deployments pointed at storage the panel must not change: a
	// staging instance reading a production gateway, or an audit/read-only
	// console. Permission checks alone are not sufficient there, because a user
	// may legitimately hold write or owner on a bucket and the panel would honour
	// it. This is a deployment-level statement that overrides all of them.
	ReadOnly bool `json:"read_only" koanf:"read_only"`
	// ServeFrontend controls whether the embedded SPA is served. Disable it for
	// API-only instances. Defaults to true.
	ServeFrontend bool `json:"serve_frontend"  koanf:"serve_frontend"`
	// Region is the name of the region this instance serves directly (e.g.
	// "teh-1"). Requests whose "region" header matches it (or is empty) are
	// handled locally.
	Region string `json:"region"          koanf:"region"`
	// RegionEndpoints maps other region names to the base URL of their backend.
	// When the frontend is served, a request for one of these regions is
	// reverse-proxied to that backend. Only meaningful on a frontend instance.
	RegionEndpoints map[string]string `json:"region_endpoints" koanf:"region_endpoints"`
}

// IsIAMMode reports whether the panel authenticates users with OIDC rather than
// S3 credentials.
func (s ServerConfig) IsIAMMode() bool { return s.AuthMode == AuthModeIAM }

// IsReadOnly reports whether every mutating operation is refused.
func (s ServerConfig) IsReadOnly() bool { return s.ReadOnly }

type ServerCorsConfig struct {
	AllowedOrigins []string `json:"allowed_origins" koanf:"allowed_origins"`
}

type ObjectStorageConfig struct {
	URL            string `json:"url"         koanf:"url"`
	AccessKeyAdmin string `json:"access_key"  koanf:"access_key"`
	SecretKeyAdmin string `json:"secret_key"  koanf:"secret_key"`
	// ControlURL is a second S3-compatible endpoint used ONLY for authorization
	// metadata: which buckets the caller may use, their usage, and their bucket
	// policies. Object operations keep going to URL.
	//
	// Splitting the two is what lets an authorization service answer "which
	// buckets, and with what permission" across every region at once, without
	// ever standing in the path of object bytes. Required in AuthModeIAM.
	ControlURL string `json:"control_url" koanf:"control_url"`
	// STSURL is the endpoint that vends the short-lived credentials object calls
	// are signed with (AWS STS GetSessionToken). Defaults to ControlURL with its
	// last path segment replaced by "sts". AuthModeIAM only.
	STSURL string `json:"sts_url" koanf:"sts_url"`
}

// OIDCConfig configures the browser login flow used in AuthModeIAM.
type OIDCConfig struct {
	IssuerURL    string `json:"issuer_url"    koanf:"issuer_url"`
	ClientID     string `json:"client_id"     koanf:"client_id"`
	ClientSecret string `json:"client_secret" koanf:"client_secret"`
	// RedirectURL must match the client's registered redirect URI and point at
	// this panel's /auth/callback.
	RedirectURL string `json:"redirect_url" koanf:"redirect_url"`
	// CookieKey is the base64 of 32 random bytes used to encrypt the session
	// cookie (openssl rand -base64 32). Required in AuthModeIAM.
	CookieKey  string `json:"cookie_key"  koanf:"cookie_key"`
	CookieName string `json:"cookie_name" koanf:"cookie_name"`
	// CookieSecure marks the session cookie Secure. Defaults to true; set false
	// only for plain-HTTP local development.
	CookieSecure bool `json:"cookie_secure" koanf:"cookie_secure"`
	// Scopes requested at login. "openid" is always included.
	Scopes []string `json:"scopes" koanf:"scopes"`
	// GroupsClaim is the token claim holding the user's groups.
	GroupsClaim string `json:"groups_claim" koanf:"groups_claim"`
	// AdminGroups are the groups whose members get the panel's admin view. The
	// control endpoint still decides what they may see; this only reveals the UI.
	AdminGroups []string `json:"admin_groups" koanf:"admin_groups"`
	// PostLoginRedirect is where the browser lands after a successful login.
	PostLoginRedirect string `json:"post_login_redirect" koanf:"post_login_redirect"`
	// SessionTTL is how long a sign-in lasts before the user must authenticate
	// again. It is independent of the access token's lifetime, which is short
	// (minutes) and refreshed underneath the session. Defaults to 12h.
	SessionTTL time.Duration `json:"session_ttl" koanf:"session_ttl"`
}
