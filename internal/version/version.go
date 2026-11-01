// Package version reports which build of the panel is running.
package version

import "runtime/debug"

// Version is stamped at build time:
//
//	go build -ldflags "-X github.com/snapp-incubator/S3-Panel/internal/version.Version=v1.9.0"
//
// Left empty, Get falls back to the module version Go recorded, then "dev".
var Version string

// Get returns the running build's version.
func Get() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
