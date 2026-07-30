package stats

import (
	"fmt"
	"sync"
	"testing"
)

func findNode(root *FileNode, path string) *FileNode {
	if root == nil {
		return nil
	}
	if root.Path == path {
		return root
	}
	for _, child := range root.Children {
		if found := findNode(child, path); found != nil {
			return found
		}
	}
	return nil
}

func tree(t *testing.T, tracker *Tracker) *FileNode {
	t.Helper()
	root, ok := tracker.GetFileTree().(*FileNode)
	if !ok {
		t.Fatalf("GetFileTree returned %T, want *FileNode", tracker.GetFileTree())
	}
	return root
}

func TestNewBuildsNestedTree(t *testing.T) {
	tracker := New("myrepo", []string{
		"main.go",
		"pkg/server/api.go",
		"pkg/server/ws.go",
		"pkg/hub/hub.go",
	})

	root := tree(t, tracker)
	if root.Name != "myrepo" || !root.IsDir {
		t.Errorf("unexpected root node: %+v", root)
	}

	pkg := findNode(root, "pkg")
	if pkg == nil || !pkg.IsDir {
		t.Fatalf("expected directory node 'pkg', got %+v", pkg)
	}
	if len(pkg.Children) != 2 {
		t.Errorf("expected pkg to have 2 subdirectories, got %d", len(pkg.Children))
	}

	api := findNode(root, "pkg/server/api.go")
	if api == nil {
		t.Fatal("expected leaf node 'pkg/server/api.go'")
	}
	if api.IsDir {
		t.Error("expected api.go to be a file node")
	}
	if api.Name != "api.go" {
		t.Errorf("expected leaf name 'api.go', got %q", api.Name)
	}
}

func TestDirectoriesSortBeforeFiles(t *testing.T) {
	tracker := New("repo", []string{"zeta.go", "alpha/one.go", "beta.go"})

	root := tree(t, tracker)
	if len(root.Children) != 3 {
		t.Fatalf("expected 3 top-level children, got %d", len(root.Children))
	}
	if !root.Children[0].IsDir || root.Children[0].Name != "alpha" {
		t.Errorf("expected the directory first, got %+v", root.Children[0])
	}
	if root.Children[1].Name != "beta.go" || root.Children[2].Name != "zeta.go" {
		t.Errorf("expected files in alphabetical order, got %s then %s",
			root.Children[1].Name, root.Children[2].Name)
	}
}

func TestRecordEditAccumulates(t *testing.T) {
	tracker := New("repo", []string{"main.go"})

	count, added, removed := tracker.RecordEdit("main.go", 10, 3)
	if count != 1 || added != 10 || removed != 3 {
		t.Fatalf("first edit: got count=%d added=%d removed=%d", count, added, removed)
	}

	count, added, removed = tracker.RecordEdit("main.go", 5, 1)
	if count != 2 || added != 15 || removed != 4 {
		t.Fatalf("second edit: got count=%d added=%d removed=%d", count, added, removed)
	}

	node := findNode(tree(t, tracker), "main.go")
	if node.EditCount != 2 {
		t.Errorf("expected editCount 2 in the tree snapshot, got %d", node.EditCount)
	}

	totalAdded, totalRemoved := tracker.GetLOCDelta()
	if totalAdded != 15 || totalRemoved != 4 {
		t.Errorf("GetLOCDelta: got %d/%d, want 15/4", totalAdded, totalRemoved)
	}
}

// Files created after startup are not in the baseline copy, so the tracker has
// to graft them into the tree on their first diff.
func TestRecordEditInsertsUnknownFile(t *testing.T) {
	tracker := New("repo", []string{"main.go"})

	count, _, _ := tracker.RecordEdit("pkg/new/thing.go", 4, 0)
	if count != 1 {
		t.Fatalf("expected editCount 1 for the new file, got %d", count)
	}

	root := tree(t, tracker)
	if node := findNode(root, "pkg/new/thing.go"); node == nil {
		t.Fatal("expected the new file to appear in the tree")
	}
	if dir := findNode(root, "pkg/new"); dir == nil || !dir.IsDir {
		t.Error("expected the intermediate directory to be created")
	}
}

func TestPathNormalization(t *testing.T) {
	tracker := New("repo", []string{"pkg/server/api.go"})

	// "./pkg/server/api.go" must land on the existing node, not create a second.
	if count, _, _ := tracker.RecordEdit("./pkg/server/api.go", 1, 0); count != 1 {
		t.Fatalf("expected the normalized path to hit the existing node, got count=%d", count)
	}
	if count, _, _ := tracker.RecordEdit("pkg/server/api.go", 1, 0); count != 2 {
		t.Fatalf("expected the same node to increment again, got count=%d", count)
	}

	root := tree(t, tracker)
	if len(root.Children) != 1 {
		t.Errorf("expected a single top-level child, got %d", len(root.Children))
	}
}

func TestLOCHistoryGrowsAndIsBounded(t *testing.T) {
	tracker := New("repo", []string{"main.go"})
	tracker.maxHistory = 10

	for i := 0; i < 40; i++ {
		tracker.RecordEdit("main.go", 1, 0)
	}

	history, ok := tracker.GetLOCHistory().([]LOCPoint)
	if !ok {
		t.Fatalf("GetLOCHistory returned %T, want []LOCPoint", tracker.GetLOCHistory())
	}
	if len(history) == 0 || len(history) > 10 {
		t.Fatalf("expected the history to stay within the cap, got %d points", len(history))
	}

	last := history[len(history)-1]
	if last.Added != 40 {
		t.Errorf("expected the newest point to carry the cumulative total 40, got %d", last.Added)
	}
	if last.Timestamp.IsZero() {
		t.Error("expected the point to be timestamped")
	}
}

// The snapshot handed to the JSON encoder must not alias the live tree.
func TestGetFileTreeReturnsDeepCopy(t *testing.T) {
	tracker := New("repo", []string{"main.go"})
	tracker.RecordEdit("main.go", 1, 0)

	snapshot := tree(t, tracker)
	tracker.RecordEdit("main.go", 1, 0)

	if node := findNode(snapshot, "main.go"); node.EditCount != 1 {
		t.Errorf("snapshot changed after a later edit: got editCount %d, want 1", node.EditCount)
	}
}

func TestConcurrentRecordAndRead(t *testing.T) {
	tracker := New("repo", []string{"main.go"})

	const writers, edits = 8, 50
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < edits; i++ {
				tracker.RecordEdit(fmt.Sprintf("pkg/w%d/file.go", id), 1, 1)
			}
		}(w)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = tracker.GetFileTree()
			_, _ = tracker.GetLOCDelta()
			_ = tracker.GetLOCHistory()
		}
	}()

	wg.Wait()

	added, removed := tracker.GetLOCDelta()
	if added != writers*edits || removed != writers*edits {
		t.Errorf("expected %d added/removed, got %d/%d", writers*edits, added, removed)
	}
	for w := 0; w < writers; w++ {
		path := fmt.Sprintf("pkg/w%d/file.go", w)
		if got := tracker.EditCount(path); got != edits {
			t.Errorf("%s: expected %d edits, got %d", path, edits, got)
		}
	}
}

func TestEmptyAndDotPathsAreIgnored(t *testing.T) {
	tracker := New("", nil)

	if count, _, _ := tracker.RecordEdit("", 5, 5); count != 0 {
		t.Errorf("expected an empty path to be ignored, got count=%d", count)
	}
	if count, _, _ := tracker.RecordEdit(".", 5, 5); count != 0 {
		t.Errorf("expected '.' to be ignored, got count=%d", count)
	}

	added, removed := tracker.GetLOCDelta()
	if added != 0 || removed != 0 {
		t.Errorf("expected ignored paths not to move the totals, got %d/%d", added, removed)
	}

	root := tree(t, tracker)
	if root.Name != "workspace" {
		t.Errorf("expected the default root name 'workspace', got %q", root.Name)
	}
}
