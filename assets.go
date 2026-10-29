// Package dedo holds what a service copies from dedo: the templates and the
// guidance for coding agents. The dedo command (cmd/dedo) writes them into a
// repository.
package dedo

import "embed"

// Files holds templates/, guidance/claude.md and guidance/generators.md.
//
//go:embed templates guidance/claude.md guidance/generators.md
var Files embed.FS
