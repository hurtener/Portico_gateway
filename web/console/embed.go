// Package console exposes the SvelteKit Console build output as an
// embed.FS so the Go binary can serve the SPA without depending on
// disk layout.
//
// The build directory is populated by `npm run build` (CI runs this
// before `go build`); a placeholder `.gitkeep` keeps the path embeddable
// even when the frontend has not been built yet.
package console

import "embed"

//go:embed all:build
var Build embed.FS
