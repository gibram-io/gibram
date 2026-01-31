// Package version provides the shared version string for GibRAM components.
package version

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var rawVersion string

// Version is the trimmed semantic version (e.g., "0.2.0").
var Version = strings.TrimSpace(rawVersion)
