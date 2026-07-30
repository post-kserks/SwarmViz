package frontend

import (
	"embed"
	"io/fs"
)

// Dist embeds the compiled static assets from frontend/dist
//
//go:embed all:dist
var Dist embed.FS

// GetFS returns an fs.FS rooted at the dist directory.
func GetFS() (fs.FS, error) {
	return fs.Sub(Dist, "dist")
}
