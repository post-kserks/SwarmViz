package frontend

import (
	"io/fs"
	"testing"
)

func TestGetFS(t *testing.T) {
	subFS, err := GetFS()
	if err != nil {
		t.Fatalf("GetFS() returned error: %v", err)
	}

	if !IsBuilt() {
		t.Skip("frontend/dist is empty; run `npm --prefix frontend run build` to embed the UI")
	}

	index, err := subFS.Open("index.html")
	if err != nil {
		t.Fatalf("Failed to open index.html from embedded FS: %v", err)
	}
	defer index.Close()

	stat, err := index.Stat()
	if err != nil {
		t.Fatalf("Failed to stat index.html: %v", err)
	}

	if stat.Size() == 0 {
		t.Errorf("Expected index.html size > 0, got 0")
	}

	assets, err := fs.ReadDir(subFS, "assets")
	if err != nil {
		t.Fatalf("Failed to read assets directory from embedded FS: %v", err)
	}

	if len(assets) == 0 {
		t.Errorf("Expected non-empty assets directory in embedded FS")
	}
}
