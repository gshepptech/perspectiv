// Package web carries the built single-page app so the server binary serves
// it without a separate static file host.
//
// Dist holds web/dist, the output of `pnpm run build` (make web-build), under
// the "dist" directory. The bundle must exist when the Go code compiles; the
// Makefile's build target and the image build produce it first.
package web

import "embed"

// Dist is the built SPA; fs.Sub(Dist, "dist") is its root.
//
//go:embed all:dist
var Dist embed.FS
