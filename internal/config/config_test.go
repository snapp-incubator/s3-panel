package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestProvideRegionEndpoints ensures the [server.region_endpoints] sub-table
// (with a dashed key) round-trips through koanf into the map.
func TestProvideRegionEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[server]
region = "teh-1"
serve_frontend = true

[server.region_endpoints]
"teh-2" = "http://s3-panel.apps.private.okd4.teh-2.snappcloud.io"

[object_storage_config]
url = "https://s3.teh-1.snappcloud.io"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Provide(path)

	if cfg.Server.Region != "teh-1" {
		t.Fatalf("region: got %q, want teh-1", cfg.Server.Region)
	}
	want := "http://s3-panel.apps.private.okd4.teh-2.snappcloud.io"
	if got := cfg.Server.RegionEndpoints["teh-2"]; got != want {
		t.Fatalf("region_endpoints[teh-2]: got %q, want %q", got, want)
	}
	if !cfg.Server.ServeFrontend {
		t.Fatalf("serve_frontend: got false, want true")
	}
}

func TestBasePathNormalises(t *testing.T) {
	for in, want := range map[string]string{
		"":           "",
		"/":          "",
		"  ":         "",
		"storage":    "/storage",
		"/storage":   "/storage",
		"/storage/":  "/storage",
		"//storage/": "/storage",
		"/a/b/":      "/a/b",
	} {
		s := ServerConfig{BasePath: in}
		if got := s.Base(); got != want {
			t.Errorf("Base(%q) = %q, want %q", in, got, want)
		}
		if err := s.ValidateBasePath(); err != nil {
			t.Errorf("ValidateBasePath(%q) = %v, want nil", in, err)
		}
	}
	for _, bad := range []string{"/a/../b", "/a//b", "/a/./b", "/st orage", "/st%2forage", "/a?b", "/a#b", `/a\b`} {
		if err := (ServerConfig{BasePath: bad}).ValidateBasePath(); err == nil {
			t.Errorf("ValidateBasePath(%q) = nil, want an error", bad)
		}
	}
}

// TestProvidePanelTables: nav and links are arrays of tables in TOML, and must
// arrive as such; the app key comes from the environment like other secrets.
func TestProvidePanelTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[server]
base_path = "/storage"

[panel]
enabled = true

[[panel.nav]]
label = "Buckets"
path = "/storage/object-storage/s3-bucket/buckets"

[[panel.links]]
resource = "ceph-*|bucket"
path = "/storage/iam/buckets/{name}?region={region}"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("s3panel_panel__api_key", "app_from_env")

	cfg := Provide(path)
	p := cfg.Panel.WithDefaults()
	if !p.Enabled || p.APIKey != "app_from_env" {
		t.Errorf("panel = %+v", p)
	}
	if len(p.Nav) != 1 || p.Nav[0].Label != "Buckets" || len(p.Links) != 1 || p.Links[0].Resource != "ceph-*|bucket" {
		t.Errorf("nav/links = %+v / %+v", p.Nav, p.Links)
	}
	if p.AppKey != DefaultPanelAppKey || p.HeaderScript != DefaultPanelHeaderScript || p.RegisterInterval != DefaultPanelRegisterInterval {
		t.Errorf("defaults not applied: %+v", p)
	}
	if DefaultConfig().Panel.Enabled {
		t.Error("the panel integration is on by default")
	}
}

// TestProvideShareMaxDuration: the share cap is a Go duration from the file,
// overridable from the environment, and absent by default.
func TestProvideShareMaxDuration(t *testing.T) {
	if got := DefaultConfig().Share.MaxDuration; got != 0 {
		t.Fatalf("default share.max_duration = %v, want 0 (no cap)", got)
	}

	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(empty, []byte("[server]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Provide(empty).Share.MaxDuration; got != 0 {
		t.Errorf("unset share.max_duration = %v, want 0", got)
	}

	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[share]\nmax_duration = \"30m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Provide(path).Share.MaxDuration; got != 30*time.Minute {
		t.Errorf("share.max_duration from file = %v, want 30m", got)
	}

	t.Setenv("s3panel_share__max_duration", "2h")
	if got := Provide(path).Share.MaxDuration; got != 2*time.Hour {
		t.Errorf("share.max_duration from env = %v, want 2h", got)
	}
}

func TestShareConfigValidate(t *testing.T) {
	for _, ok := range []time.Duration{0, time.Minute, 15 * time.Minute, 14 * 24 * time.Hour} {
		if err := (ShareConfig{MaxDuration: ok}).Validate(); err != nil {
			t.Errorf("Validate(%v) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []time.Duration{-time.Minute, time.Nanosecond, 30 * time.Second} {
		if err := (ShareConfig{MaxDuration: bad}).Validate(); err == nil {
			t.Errorf("Validate(%v) = nil, want an error", bad)
		}
	}
}

// TestProvideOIDCPanelSwitches: bind_session and revoke_refresh_token follow
// panel.enabled until set, and an explicit false wins over the panel.
func TestProvideOIDCPanelSwitches(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	unset := Provide(write("unset.toml", "[server]\n")).OIDC
	if unset.BindSession != nil || unset.RevokeRefreshToken != nil {
		t.Fatalf("unset switches = %v, %v, want nil", unset.BindSession, unset.RevokeRefreshToken)
	}
	if unset.BindsSession(false) || unset.RevokesRefreshToken(false) {
		t.Error("standalone: a switch is on by default")
	}
	if !unset.BindsSession(true) || !unset.RevokesRefreshToken(true) {
		t.Error("panel: a switch is off by default")
	}

	off := Provide(write("off.toml", "[oidc]\nbind_session = false\nrevoke_refresh_token = false\n")).OIDC
	if off.BindsSession(true) || off.RevokesRefreshToken(true) {
		t.Error("an explicit false did not win over the panel")
	}

	t.Setenv("s3panel_oidc__bind_session", "true")
	if !Provide(write("env.toml", "[server]\n")).OIDC.BindsSession(false) {
		t.Error("bind_session from env did not turn it on standalone")
	}
}
