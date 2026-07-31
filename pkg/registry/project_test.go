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

func TestFromPathsAcceptsRepositoriesAnywhere(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	mkRepo(t, rootA, "vault")
	mkRepo(t, rootB, "notes")

	projects, err := FromPaths([]string{
		filepath.Join(rootA, "vault"),
		"  ",
		filepath.Join(rootB, "notes"),
	})
	if err != nil {
		t.Fatalf("FromPaths: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %+v", len(projects), projects)
	}
	if projects[0].ID != "vault" || projects[1].ID != "notes" {
		t.Fatalf("unexpected ids: %+v", projects)
	}
}

func TestFromPathsRejectsNonRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := FromPaths([]string{filepath.Join(root, "plain")}); err == nil {
		t.Fatal("expected an error for a non-git path, got nil")
	}
}

func TestMergeDropsDuplicatePathsAndResolvesIDs(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, "alpha")
	other := t.TempDir()
	mkRepo(t, other, "alpha")

	discovered, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	explicit, err := FromPaths([]string{
		filepath.Join(root, "alpha"),  // same path as discovered — must be dropped
		filepath.Join(other, "alpha"), // same slug, different path — must be renamed
	})
	if err != nil {
		t.Fatalf("FromPaths: %v", err)
	}

	merged := Merge(discovered, explicit)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged projects, got %d: %+v", len(merged), merged)
	}
	if merged[0].ID == merged[1].ID {
		t.Fatalf("expected unique ids after merge, got %+v", merged)
	}
	if merged[0].Path == merged[1].Path {
		t.Fatalf("expected unique paths after merge, got %+v", merged)
	}
}

func TestExcludeDropsRootProject(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, "alpha")
	mkRepo(t, root, "beta")

	projects, err := Discover(root, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	kept := Exclude(projects, filepath.Join(root, "alpha"))
	if len(kept) != 1 || kept[0].Name != "beta" {
		t.Fatalf("expected only beta to survive, got %+v", kept)
	}

	// A path that matches nothing must leave the list untouched.
	if got := Exclude(projects, filepath.Join(root, "nope")); len(got) != 2 {
		t.Fatalf("expected both projects kept, got %+v", got)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"SwarmViz":   "swarmviz",
		"Foo Bar":    "foo-bar",
		"foo__bar":   "foo-bar",
		"---":        "project",
		"already-ok": "already-ok",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
