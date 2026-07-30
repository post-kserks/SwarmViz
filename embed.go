package main

import (
	"embed"
	"io/fs"

	"github.com/swarmviz/swarmviz/frontend"
)

// FrontendDist embeds the frontend build output from frontend/dist
//
//go:embed all:frontend/dist
var FrontendDist embed.FS

// GetFrontendFS returns an fs.FS rooted at "frontend/dist"
func GetFrontendFS() (fs.FS, error) {
	return frontend.GetFS()
}
