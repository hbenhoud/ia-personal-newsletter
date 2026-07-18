// Package templates embeds the project's templates so the ingest and server
// binaries are fully self-contained (no reliance on the working directory).
//
// The frozen static CLI (cmd/newsletter) still reads these files from disk via
// relative paths; this embed is additive and does not change that.
package templates

import "embed"

// FS holds the LLM prompt, theme CSS, static HTML, and the server-side web
// templates under web/.
//
//go:embed prompts themes web
var FS embed.FS
