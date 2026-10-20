// Package dedo holds what a service copies from dedo: the templates and the
// guidance for coding agents. The dedo command (cmd/dedo) writes them into a
// repository.
package dedo

import "embed"

// Files holds templates/ and guidance/claude.md.
//
//go:embed templates guidance/claude.md
var Files embed.FS
