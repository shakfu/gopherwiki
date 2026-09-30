package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewGitStorage(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test initialization
	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	if gs.Path() != tmpDir {
		t.Errorf("Path() = %q, want %q", gs.Path(), tmpDir)
	}

	// Check .git directory was created
	gitDir := filepath.Join(tmpDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error(".git directory was not created")
	}
}

func TestGitStorageStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	// Store a file
	author := Author{Name: "Test User", Email: "test@example.com"}
	changed, err := gs.Store("test.md", "# Hello World\n", "Initial commit", author)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	if !changed {
		t.Error("Store should report changed=true for new file")
	}

	// Verify file exists
	if !gs.Exists("test.md") {
		t.Error("File should exist after store")
	}

	// Load and verify content
	content, err := gs.Load("test.md", "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if content != "# Hello World\n" {
		t.Errorf("Load() = %q, want %q", content, "# Hello World\n")
	}
}

func TestGitStorageHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	author := Author{Name: "Test User", Email: "test@example.com"}

	// Create initial file
	gs.Store("test.md", "# Version 1\n", "Version 1", author)

	// Update file
	gs.Store("test.md", "# Version 2\n", "Version 2", author)

	// Get history
	log, err := gs.Log("test.md", 10)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	if len(log) != 2 {
		t.Errorf("Log length = %d, want 2", len(log))
	}

	if log[0].Message != "Version 2" {
		t.Errorf("First log entry message = %q, want %q", log[0].Message, "Version 2")
	}
}

func TestGitStorageDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	author := Author{Name: "Test User", Email: "test@example.com"}

	// Create file
	gs.Store("test.md", "# Test\n", "Create", author)

	if !gs.Exists("test.md") {
		t.Fatal("File should exist after store")
	}

	// Delete file
	err = gs.Delete("test.md", "Delete test", author)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if gs.Exists("test.md") {
		t.Error("File should not exist after delete")
	}
}

func TestGitStorageRename(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	author := Author{Name: "Test User", Email: "test@example.com"}

	// Create file
	gs.Store("old.md", "# Test\n", "Create", author)

	// Rename file
	err = gs.Rename("old.md", "new.md", "Rename test", author)
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	if gs.Exists("old.md") {
		t.Error("Old file should not exist after rename")
	}

	if !gs.Exists("new.md") {
		t.Error("New file should exist after rename")
	}

	// Verify content
	content, _ := gs.Load("new.md", "")
	if content != "# Test\n" {
		t.Errorf("Content after rename = %q, want %q", content, "# Test\n")
	}
}

func TestGitStorageBlame(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	author := Author{Name: "Test User", Email: "test@example.com"}

	// Create file with multiple lines
	gs.Store("test.md", "Line 1\nLine 2\nLine 3\n", "Create", author)

	// Get blame
	blame, err := gs.Blame("test.md", "")
	if err != nil {
		t.Fatalf("Blame failed: %v", err)
	}

	if len(blame) != 3 {
		t.Errorf("Blame length = %d, want 3", len(blame))
	}

	for i, line := range blame {
		if line.LineNumber != i+1 {
			t.Errorf("Line %d number = %d, want %d", i, line.LineNumber, i+1)
		}
	}
}

func TestPathTraversalProtection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	author := Author{Name: "Test User", Email: "test@example.com"}

	// Store a legitimate file first
	gs.Store("test.md", "# Test\n", "Create", author)

	traversalPaths := []string{
		"../etc/passwd",
		"../../etc/shadow",
		"foo/../../etc/passwd",
		"/etc/passwd",
		"/absolute/path.md",
	}

	for _, p := range traversalPaths {
		t.Run("Exists_"+p, func(t *testing.T) {
			if gs.Exists(p) {
				t.Errorf("Exists(%q) should return false for traversal path", p)
			}
		})

		t.Run("Load_"+p, func(t *testing.T) {
			_, err := gs.Load(p, "")
			if !errors.Is(err, ErrPathTraversal) {
				t.Errorf("Load(%q) should return ErrPathTraversal, got: %v", p, err)
			}
		})

		t.Run("Store_"+p, func(t *testing.T) {
			_, err := gs.Store(p, "malicious", "bad", author)
			if !errors.Is(err, ErrPathTraversal) {
				t.Errorf("Store(%q) should return ErrPathTraversal, got: %v", p, err)
			}
		})

		t.Run("Delete_"+p, func(t *testing.T) {
			err := gs.Delete(p, "bad", author)
			if !errors.Is(err, ErrPathTraversal) {
				t.Errorf("Delete(%q) should return ErrPathTraversal, got: %v", p, err)
			}
		})

		t.Run("Mtime_"+p, func(t *testing.T) {
			_, err := gs.Mtime(p)
			if !errors.Is(err, ErrPathTraversal) {
				t.Errorf("Mtime(%q) should return ErrPathTraversal, got: %v", p, err)
			}
		})

		t.Run("Size_"+p, func(t *testing.T) {
			_, err := gs.Size(p)
			if !errors.Is(err, ErrPathTraversal) {
				t.Errorf("Size(%q) should return ErrPathTraversal, got: %v", p, err)
			}
		})
	}

	// Legitimate paths should still work
	t.Run("legitimate_path", func(t *testing.T) {
		if !gs.Exists("test.md") {
			t.Error("Exists(test.md) should return true")
		}
		content, err := gs.Load("test.md", "")
		if err != nil {
			t.Errorf("Load(test.md) should succeed, got: %v", err)
		}
		if content != "# Test\n" {
			t.Errorf("Load(test.md) = %q, want %q", content, "# Test\n")
		}
	})

	t.Run("legitimate_subdirectory", func(t *testing.T) {
		_, err := gs.Store("sub/page.md", "# Sub\n", "Create sub", author)
		if err != nil {
			t.Errorf("Store(sub/page.md) should succeed, got: %v", err)
		}
		if !gs.Exists("sub/page.md") {
			t.Error("Exists(sub/page.md) should return true")
		}
	})
}

func TestGitStorageConcurrentWrites(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	const numGoroutines = 10
	author := Author{Name: "Test User", Email: "test@example.com"}

	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			filename := fmt.Sprintf("concurrent-%d.md", n)
			content := fmt.Sprintf("# Page %d\n", n)
			message := fmt.Sprintf("Create page %d", n)
			_, err := gs.Store(filename, content, message, author)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d Store failed: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Verify all files were created with correct content
	for i := 0; i < numGoroutines; i++ {
		filename := fmt.Sprintf("concurrent-%d.md", i)
		expected := fmt.Sprintf("# Page %d\n", i)

		if !gs.Exists(filename) {
			t.Errorf("File %s should exist", filename)
			continue
		}

		content, err := gs.Load(filename, "")
		if err != nil {
			t.Errorf("Load(%s) failed: %v", filename, err)
			continue
		}

		if content != expected {
			t.Errorf("Load(%s) = %q, want %q", filename, content, expected)
		}
	}
}

func TestMetadataDoesNotComputeFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gopherwiki-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gs, err := NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	author := Author{Name: "Test User", Email: "test@example.com"}
	gs.Store("page.md", "# Hello\n", "Create page", author)

	meta, err := gs.Metadata("page.md", "")
	if err != nil {
		t.Fatalf("Metadata failed: %v", err)
	}
	if meta.Files != nil {
		t.Errorf("Metadata().Files should be nil (lazy), got %v", meta.Files)
	}

	// ShowCommit should still populate Files
	commitMeta, _, err := gs.ShowCommit(meta.Revision)
	if err != nil {
		t.Fatalf("ShowCommit failed: %v", err)
	}
	if len(commitMeta.Files) == 0 {
		t.Error("ShowCommit().Files should be populated")
	}
}

func TestStoreFilesOneCommit(t *testing.T) {
	gs, err := NewGitStorage(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	author := Author{Name: "Agent", Email: "agent@example.com"}
	gs.Store("kb/old.md", "old", "init", author)

	changed, err := gs.StoreFiles(map[string][]byte{
		"kb/old.md":   []byte("updated"),
		"kb/new/a.md": []byte("a"),
		"kb/new/b.md": []byte("b"),
	}, "agent run", author)
	if err != nil || !changed {
		t.Fatalf("StoreFiles = %v, %v; want true, nil", changed, err)
	}

	log, _ := gs.Log("", 0)
	if len(log) != 2 || log[0].Message != "agent run" {
		t.Fatalf("log = %+v, want one new commit", log)
	}
	meta, _, err := gs.ShowCommit(log[0].Revision)
	if err != nil || len(meta.Files) != 3 {
		t.Errorf("commit files = %v, %v; want all three", meta.Files, err)
	}

	changed, err = gs.StoreFiles(map[string][]byte{"kb/old.md": []byte("updated")}, "no-op", author)
	if err != nil || changed {
		t.Errorf("unchanged StoreFiles = %v, %v; want false, nil", changed, err)
	}
	if log, _ := gs.Log("", 0); len(log) != 2 {
		t.Error("an unchanged batch must not commit")
	}
}

func TestStoreFilesRejectsTraversalBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGitStorage(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gs.StoreFiles(map[string][]byte{"a.md": []byte("a"), "../escape.md": []byte("x")}, "m", Author{Name: "a", Email: "a@a"})
	if !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("err = %v, want ErrPathTraversal", err)
	}
	if gs.Exists("a.md") {
		t.Error("no file may be written when one path is rejected")
	}
}

func TestStoreFilesRollsBackOnFailure(t *testing.T) {
	gs, err := NewGitStorage(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	author := Author{Name: "a", Email: "a@a"}
	gs.Store("a.md", "original", "init", author)
	gs.Store("blocker", "a file where a directory is needed", "init", author)

	// "blocker/x.md" cannot be written because "blocker" is a file. The files
	// sort as a.md, blocker/x.md, so a.md is written first and must be restored.
	_, err = gs.StoreFiles(map[string][]byte{"a.md": []byte("changed"), "blocker/x.md": []byte("x")}, "m", author)
	if err == nil {
		t.Fatal("StoreFiles should fail")
	}
	if content, _ := gs.Load("a.md", ""); content != "original" {
		t.Errorf("a.md = %q, want the original content restored", content)
	}

	// The worktree is clean: a later single-file store commits only its file.
	gs.Store("c.md", "c", "later", author)
	log, _ := gs.Log("", 1)
	meta, _, _ := gs.ShowCommit(log[0].Revision)
	if len(meta.Files) != 1 || meta.Files[0] != "c.md" {
		t.Errorf("later commit files = %v, want only c.md", meta.Files)
	}
}

func TestStoreUnchangedContent(t *testing.T) {
	gs, err := NewGitStorage(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	author := Author{Name: "a", Email: "a@a"}
	if changed, err := gs.Store("x.md", "same", "first", author); err != nil || !changed {
		t.Fatalf("first Store = %v, %v; want true, nil", changed, err)
	}
	// Saving identical content is a no-op, not an "empty commit" error.
	if changed, err := gs.Store("x.md", "same", "second", author); err != nil || changed {
		t.Errorf("second Store = %v, %v; want false, nil", changed, err)
	}
}
