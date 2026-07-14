package main

import (
	"fmt"
	"runtime/debug"
)

// Build metadata, overridden at link time via -ldflags
// "-X main.version=... -X main.commit=... -X main.date=...".
// GoReleaser sets these from the git tag; a plain `go build` leaves the defaults.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// effectiveVersion resolves the version to report. Link-time ldflags win
// (GoReleaser sets them from the git tag). For `go install`-based builds the
// ldflags default to "dev", so fall back to the module version recorded in the
// build info. The "(devel)" placeholder and an empty version are ignored.
func effectiveVersion(ldflags string, info *debug.BuildInfo) string {
	if ldflags != "dev" {
		return ldflags
	}
	if info != nil {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return ldflags
}

// versionString renders a single-line build banner. commit and date are
// omitted when empty (e.g. local dev builds).
func versionString(version, commit, date string) string {
	s := fmt.Sprintf("driftlens %s", version)
	if commit != "" {
		s += fmt.Sprintf(" (%s)", commit)
	}
	if date != "" {
		s += fmt.Sprintf(" built %s", date)
	}
	return s
}
