package storage

import (
	"os"
	"path/filepath"
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
