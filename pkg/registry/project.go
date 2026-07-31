// Package registry discovers additional git repositories a multi-project
// SwarmViz instance can watch, alongside the always-present root project
// configured via --path.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Project struct {
	ID   string
	Name string
	Path string
}

var (
	slugInvalid = regexp.MustCompile(`[^a-z0-9-]+`)
	slugDashes  = regexp.MustCompile(`-+`)
)

// Slug turns an arbitrary directory name into a URL-path-safe id.
func Slug(name string) string {
	s := strings.ToLower(name)
	s = slugInvalid.ReplaceAllString(s, "-")
	s = slugDashes.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "project"
	}
	return s
}

// Discover lists immediate subdirectories of root that contain a .git entry.
// If allowlist is non-empty, only directories whose name matches (case
// insensitive) one of its entries are kept — this is the "watch only certain
// projects" mode; an empty allowlist watches everything found under root.
// Results are sorted by directory name for a stable listing.
func Discover(root string, allowlist []string) ([]Project, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("failed reading projects root %s: %w", root, err)
	}

	allow := make(map[string]bool, len(allowlist))
	for _, a := range allowlist {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" {
			allow[a] = true
		}
	}

	seen := make(map[string]int)
	var projects []Project

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if len(allow) > 0 && !allow[strings.ToLower(name)] {
			continue
		}

		full := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(full, ".git")); err != nil {
			continue
		}

		id := Slug(name)
		if n := seen[id]; n > 0 {
			seen[id] = n + 1
			id = fmt.Sprintf("%s-%d", id, n+1)
		} else {
			seen[id] = 1
		}

		projects = append(projects, Project{ID: id, Name: name, Path: full})
	}

	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

// FromPaths turns explicit repository paths into projects. Unlike Discover it
// takes each path as given, so repositories that do not live under a common
// parent — a notes vault next to a projects directory, say — can still be
// watched. Paths that are not git repositories are an error rather than a
// silent skip: they were named one by one, so a typo should not be swallowed.
func FromPaths(paths []string) ([]Project, error) {
	var projects []Project
	seen := make(map[string]int)

	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		full, err := filepath.Abs(raw)
		if err != nil {
			return nil, fmt.Errorf("failed resolving project path %s: %w", raw, err)
		}
		if _, err := os.Stat(filepath.Join(full, ".git")); err != nil {
			return nil, fmt.Errorf("project path %s is not a git repository", full)
		}

		name := filepath.Base(full)
		id := Slug(name)
		if n := seen[id]; n > 0 {
			seen[id] = n + 1
			id = fmt.Sprintf("%s-%d", id, n+1)
		} else {
			seen[id] = 1
		}

		projects = append(projects, Project{ID: id, Name: name, Path: full})
	}

	return projects, nil
}

// Exclude drops projects whose path is the given one. It keeps the root
// project — already served on the unprefixed routes — from also appearing as a
// discovered extra project.
func Exclude(projects []Project, path string) []Project {
	abs, err := filepath.Abs(path)
	if err != nil {
		return projects
	}
	kept := make([]Project, 0, len(projects))
	for _, p := range projects {
		if p.Path == abs {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// Merge combines project lists, dropping later entries that repeat an earlier
// path and re-slugging later entries whose id is already taken. It lets an
// explicit --project-path list sit alongside --projects-root discovery without
// either one shadowing the other's routes.
func Merge(lists ...[]Project) []Project {
	var merged []Project
	byPath := make(map[string]bool)
	byID := make(map[string]int)

	for _, list := range lists {
		for _, p := range list {
			if byPath[p.Path] {
				continue
			}
			byPath[p.Path] = true

			id := p.ID
			if n := byID[id]; n > 0 {
				byID[id] = n + 1
				id = fmt.Sprintf("%s-%d", id, n+1)
			} else {
				byID[id] = 1
			}
			p.ID = id
			merged = append(merged, p)
		}
	}

	return merged
}
