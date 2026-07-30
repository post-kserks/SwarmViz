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
