package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func mkRepo(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

func TestDiscoverFindsGitRepos(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, "Alpha")
	mkRepo(t, root, "beta")
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	projects, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %+v", len(projects), projects)
	}
	if projects[0].Name != "Alpha" || projects[1].Name != "beta" {
		t.Fatalf("unexpected order/names: %+v", projects)
	}
	if projects[0].ID != "alpha" {
		t.Fatalf("expected slugged id 'alpha', got %q", projects[0].ID)
	}
}

func TestDiscoverAllowlist(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, "Alpha")
	mkRepo(t, root, "beta")

	projects, err := Discover(root, []string{"beta"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "beta" {
		t.Fatalf("expected only beta, got %+v", projects)
	}
}

func TestDiscoverDedupesSlugCollisions(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, "Foo Bar")
	mkRepo(t, root, "Foo-Bar")

	projects, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %+v", len(projects), projects)
	}
	ids := map[string]bool{projects[0].ID: true, projects[1].ID: true}
	if len(ids) != 2 {
		t.Fatalf("expected unique ids, got %+v", projects)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"SwarmViz":  "swarmviz",
		"Foo Bar":   "foo-bar",
		"foo__bar":  "foo-bar",
		"---":       "project",
		"already-ok": "already-ok",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
