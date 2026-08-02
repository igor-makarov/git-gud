// Package gitgud contains documentation embedded by the git-gud executable.
package gitgud

import _ "embed"

// README is the user documentation embedded at build time.
//
//go:embed README.md
var README string
