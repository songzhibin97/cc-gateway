package web

import "embed"

// DistFS holds the compiled frontend assets.
//
//go:embed all:dist
var DistFS embed.FS
