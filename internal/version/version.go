// Package version carries the panel's build identity.
//
// Version is the one place the product version is written down. Release
// builds override it without touching source:
//
//	go build -ldflags "-X tgwebproxy/internal/version.Version=1.2.3" ./cmd/panel
//
// It is served publicly by GET /api/v1/status/public, so it must stay a bare
// version string - never a build host, path or branch name.
package version

// Version is the panel version, overridable at link time.
var Version = "1.1.0"
