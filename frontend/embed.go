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

// IsBuilt reports whether a compiled UI is actually embedded in this binary.
// dist/ is git-ignored (only a .gitkeep placeholder is committed so that the
// //go:embed pattern always resolves), so a binary built without running the
// frontend build embeds an empty directory rather than failing to compile.
func IsBuilt() bool {
	sub, err := GetFS()
	if err != nil {
		return false
	}
	f, err := sub.Open("index.html")
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	return err == nil && stat.Size() > 0
}
