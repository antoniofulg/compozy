package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSnapshotIntegrityContract covers IT-011: a real Git repository is compared
// across HEAD, index tree, tracked, untracked, dirty, and same-content states,
// with contained relative evidence and correct divergence behavior.
func TestSnapshotIntegrityContract(t *testing.T) {
	t.Parallel()

	t.Run("Should produce equal snapshots for identical state", func(t *testing.T) {
		t.Parallel()
		requireScopeGit(t)
		root := initScopeGitRepo(t)
		first := mustCapture(t, root)
		second := mustCapture(t, root)
		if !first.Equal(second) {
			t.Fatalf("identical state not equal: %s vs %s", first.Digest(), second.Digest())
		}
		if first.IndexTree() == "" {
			t.Fatal("index tree hash missing from a committed repository")
		}
		if first.IndexTree() != second.IndexTree() {
			t.Fatalf("index tree drifted for identical state: %s vs %s", first.IndexTree(), second.IndexTree())
		}
		if first.Head() == "" {
			t.Fatal("HEAD missing from a committed repository")
		}
	})

	t.Run("Should diverge when HEAD advances", func(t *testing.T) {
		t.Parallel()
		requireScopeGit(t)
		root := initScopeGitRepo(t)
		before := mustCapture(t, root)
		writeFile(t, filepath.Join(root, "second.txt"), "second\n")
		mustScopeGit(t, root, "add", "second.txt")
		mustScopeGit(t, root, "commit", "-q", "-m", "second")
		after := mustCapture(t, root)
		if before.Equal(after) {
			t.Fatal("snapshot did not diverge after HEAD advanced")
		}
		if before.Head() == after.Head() {
			t.Fatal("HEAD unchanged after a new commit")
		}
	})

	t.Run("Should capture and update the index tree on staging", func(t *testing.T) {
		t.Parallel()
		requireScopeGit(t)
		root := initScopeGitRepo(t)
		clean := mustCapture(t, root)
		writeFile(t, filepath.Join(root, "staged.txt"), "staged\n")
		mustScopeGit(t, root, "add", "staged.txt")
		staged := mustCapture(t, root)
		if clean.IndexTree() == staged.IndexTree() {
			t.Fatalf("index tree unchanged after staging: %s", staged.IndexTree())
		}
		if clean.Equal(staged) {
			t.Fatal("snapshot digest unchanged after staging a new file")
		}
	})

	t.Run("Should detect tracked content change but not identical rewrites", func(t *testing.T) {
		t.Parallel()
		requireScopeGit(t)
		root := initScopeGitRepo(t)
		tracked := filepath.Join(root, "lines.txt")
		writeFile(t, tracked, "a\nb\nc\n")
		mustScopeGit(t, root, "add", "lines.txt")
		mustScopeGit(t, root, "commit", "-q", "-m", "lines")
		baseline := mustCapture(t, root)

		writeFile(t, tracked, "a\nb\nc\n") // byte-identical rewrite
		rewritten := mustCapture(t, root)
		if !baseline.Equal(rewritten) {
			t.Fatal("identical-content rewrite diverged from baseline")
		}

		writeFile(t, tracked, "a\nCHANGED\nc\n")
		changed := mustCapture(t, root)
		if baseline.Equal(changed) {
			t.Fatal("tracked content change did not diverge")
		}
	})

	t.Run("Should detect untracked dirty files with contained relative evidence", func(t *testing.T) {
		t.Parallel()
		requireScopeGit(t)
		root := initScopeGitRepo(t)
		clean := mustCapture(t, root)
		writeFile(t, filepath.Join(root, "produced.txt"), "agent output\n")
		dirty := mustCapture(t, root)
		if clean.Equal(dirty) {
			t.Fatal("untracked dirty file did not diverge from clean state")
		}
		found := false
		for _, entry := range dirty.Entries() {
			if filepath.IsAbs(entry.Path) {
				t.Fatalf("snapshot evidence path is absolute: %q", entry.Path)
			}
			if entry.Path == "produced.txt" {
				found = true
				if entry.Kind != "untracked" {
					t.Fatalf("produced.txt kind = %q, want untracked", entry.Kind)
				}
			}
		}
		if !found {
			t.Fatal("untracked file missing from snapshot evidence")
		}
		doc := dirty.Document()
		if doc.Head == "" || doc.Digest == "" {
			t.Fatalf("snapshot document missing head/digest: %+v", doc)
		}
	})
}

func mustCapture(t *testing.T, root string) Snapshot {
	t.Helper()
	snapshot, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatalf("Capture(%s): %v", root, err)
	}
	if !snapshot.IsSupported() {
		t.Fatalf("snapshot unsupported: %s", snapshot.UnsupportedReason())
	}
	return snapshot
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
