package config

import (
	"fmt"
	"strings"
	"time"
)

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
	// IAMConsoleURL is the browser-facing address of the IAM console, e.g.
	// "https://iam.example.com". When set, iam mode links each bucket to its
	// page there. It is not derived from the control URL, which may be an
	// in-cluster address a browser cannot reach.
	IAMConsoleURL string `json:"iam_console_url" koanf:"iam_console_url"`
	// TrustedOrigins are origins besides the panel's own that may send it
	// state-changing requests (POST, PUT, PATCH, DELETE), as
	// "scheme://host[:port]". The panel's own host is always accepted, as is a
	// request with no Origin header (a script, not a browser). Leave empty unless
	// the SPA is served from somewhere else, e.g. the Vite dev server.
	TrustedOrigins []string `json:"trusted_origins" koanf:"trusted_origins"`
	// BasePath serves the whole panel — SPA, API, sign-in and health — under a
	// path prefix of its host, e.g. "/storage" when several apps share one
	// host. The session cookie is scoped to it, and the sign-in round trip
	// stays under it. Requests reach the panel WITH the prefix: a proxy in front
	// must not strip it. The frontend must be built with the same
	// VITE_BASE_PATH. Empty (the default) serves the panel at the host's root.
	BasePath string `json:"base_path" koanf:"base_path"`
}

// IsIAMMode reports whether the panel authenticates users with OIDC rather than
// S3 credentials.
func (s ServerConfig) IsIAMMode() bool { return s.AuthMode == AuthModeIAM }

// Base returns BasePath normalised to "" (the host's root) or "/a/b": one
// leading slash, no trailing one. Validate BasePath with ValidateBasePath first.
func (s ServerConfig) Base() string {
	return strings.TrimRight("/"+strings.Trim(strings.TrimSpace(s.BasePath), "/"), "/")
}

// ValidateBasePath reports a base path that cannot be served as a plain
// prefix: one with "..", empty segments or characters outside the unreserved
// set, any of which would make the cookie Path, the redirects and the routes
// disagree about where the panel is.
func (s ServerConfig) ValidateBasePath() error {
	base := s.Base()
	if base == "" {
		return nil
	}
	for _, seg := range strings.Split(strings.TrimPrefix(base, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("server.base_path %q: empty, \".\" and \"..\" segments are not allowed", s.BasePath)
		}
		if strings.IndexFunc(seg, notUnreserved) >= 0 {
			return fmt.Errorf("server.base_path %q: only letters, digits and -._~ are allowed in a segment", s.BasePath)
		}
	}
	return nil
}

// notUnreserved reports a rune outside RFC 3986's unreserved set.
func notUnreserved(r rune) bool {
	alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	return !alnum && !strings.ContainsRune("-._~", r)
}

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
	// STSClientKey is a shared secret the control endpoint may require, marking
	// this panel as a trusted front end. It is sent on every control call: IAM
	// demands it on /sts, and on /s3 as well when its
	// awss3.require_client_key_on_s3 is on. The name predates the /s3 gate and
	// is kept so existing configs keep working.
	//
	// It is not a user credential and grants nothing on its own: the signed-in
	// person is still authenticated and still bounds what may be minted. It exists
	// so that minting is something people do THROUGH the panel — where every
	// request is authorized before anything is signed — rather than directly with
	// `aws sts get-session-token`, which would hand them an account-wide
	// credential to keep. Leave empty when the control endpoint requires none.
	STSClientKey string `json:"sts_client_key" koanf:"sts_client_key"`
	// IAMAPIURL is IAM's JSON API base, used to list debug users an admin may
	// act as. Defaults to control_url's sibling "/api".
	IAMAPIURL string `json:"iam_api_url" koanf:"iam_api_url"`
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
	// In iam mode this is only the FALLBACK: IAM's own verdict wins whenever the
	// panel could ask it, and a token without a groups claim never matches.
	AdminGroups []string `json:"admin_groups" koanf:"admin_groups"`
	// PostLoginRedirect is where the browser lands after a successful login.
	PostLoginRedirect string `json:"post_login_redirect" koanf:"post_login_redirect"`
	// SessionTTL is how long a sign-in lasts before the user must authenticate
	// again. It is independent of the access token's lifetime, which is short
	// (minutes) and refreshed underneath the session. Defaults to 12h.
	SessionTTL time.Duration `json:"session_ttl" koanf:"session_ttl"`
}

// PanelConfig joins this panel to a unified console: several apps served under
// one host, each at its own path, sharing one sign-in and one header.
//
// Off by default, and nothing in it is needed to run the panel on its own.
// When on, the panel registers itself with the console's app registry at
// startup, refuses anyone the registry says may not view it, and loads the
// console's shared header. It requires auth_mode "iam": the registry is asked
// about the person by their OIDC subject.
type PanelConfig struct {
	Enabled bool `json:"enabled" koanf:"enabled"`
	// AppKey is the key the console admitted this app under (e.g. "storage").
	// The registry identifies the app by APIKey; this names it to the header.
	AppKey string `json:"app_key" koanf:"app_key"`
	// APIURL is the console's JSON API base, ending in /api. Defaults to
	// object_storage_config.iam_api_url (or its default).
	APIURL string `json:"api_url" koanf:"api_url"`
	// APIKey is the app credential ("app_…") the console minted at admission.
	// A secret: supply it as the s3panel_panel__api_key environment variable.
	APIKey string `json:"api_key" koanf:"api_key"`
	// Description is the one-line summary the console shows for the app.
	Description string `json:"description" koanf:"description"`
	// Nav is the app's navigation in the console header. Every path must sit
	// under server.base_path. Defaults to the bucket list.
	Nav []PanelNavItem `json:"nav" koanf:"nav"`
	// Links tell the console how to open catalog resources here, e.g.
	// {resource = "ceph-*|bucket", path = "/storage/…/{name}?region={region}"}.
	// Each resource pattern must be one the app was admitted for. None by
	// default.
	Links []PanelLink `json:"links" koanf:"links"`
	// HeaderScript is the console header bundle. Defaults to
	// "/panel/header/v1/header.js".
	HeaderScript string `json:"header_script" koanf:"header_script"`
	// HomeURL and SignOutURL are the console's home and sign-out pages, linked
	// from the "No access" page and the fallback bar. Default "/" and
	// "/signout".
	HomeURL    string `json:"home_url"    koanf:"home_url"`
	SignOutURL string `json:"signout_url" koanf:"signout_url"`
	// RegisterInterval is how often the manifest is registered again after the
	// first success, so the console sees the registration as live. Defaults to
	// 6h.
	RegisterInterval time.Duration `json:"register_interval" koanf:"register_interval"`
}

// PanelNavItem is one entry of the app's navigation in the console header.
type PanelNavItem struct {
	Label string `json:"label" koanf:"label"`
	Path  string `json:"path"  koanf:"path"`
}

// PanelLink says how the console opens a catalog resource in this app.
type PanelLink struct {
	Resource string `json:"resource" koanf:"resource"`
	Path     string `json:"path"     koanf:"path"`
}

// Panel defaults, applied where the setting is left empty.
const (
	DefaultPanelAppKey           = "storage"
	DefaultPanelDescription      = "Buckets, objects and share links"
	DefaultPanelHeaderScript     = "/panel/header/v1/header.js"
	DefaultPanelHomeURL          = "/"
	DefaultPanelSignOutURL       = "/signout"
	DefaultPanelRegisterInterval = 6 * time.Hour
)

// WithDefaults fills every empty setting with its default.
func (p PanelConfig) WithDefaults() PanelConfig {
	or := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return strings.TrimSpace(v)
	}
	p.AppKey = or(p.AppKey, DefaultPanelAppKey)
	p.Description = or(p.Description, DefaultPanelDescription)
	p.HeaderScript = or(p.HeaderScript, DefaultPanelHeaderScript)
	p.HomeURL = or(p.HomeURL, DefaultPanelHomeURL)
	p.SignOutURL = or(p.SignOutURL, DefaultPanelSignOutURL)
	if p.RegisterInterval <= 0 {
		p.RegisterInterval = DefaultPanelRegisterInterval
	}
	return p
}
