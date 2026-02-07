// Package storage provides the storage abstraction for GopherWiki.
package storage

import (
	"errors"
	"time"
)

// Errors for storage operations.
var (
	ErrNotFound      = errors.New("storage: not found")
	ErrStorage       = errors.New("storage: operation failed")
	ErrPathTraversal = errors.New("storage: path traversal rejected")
)

// Author represents a commit author.
type Author struct {
	Name  string
	Email string
}

// CommitMetadata holds information about a commit.
type CommitMetadata struct {
	Revision     string
	RevisionFull string
	Datetime     time.Time
	AuthorName   string
	AuthorEmail  string
	Message      string
	Files        []string
}

// BlameLine represents a single line in a blame output.
type BlameLine struct {
	Revision   string
	AuthorName string
	Datetime   time.Time
	LineNumber int
	Line       string
	Message    string
}

// Storage defines the interface for wiki content storage.
type Storage interface {
	// Path returns the repository path.
	Path() string

	// Exists checks if a file exists.
	Exists(filename string) bool

	// IsDir checks if a path is a directory.
	IsDir(dirname string) bool

	// IsEmptyDir checks if a directory is empty.
	IsEmptyDir(dirname string) bool

	// Mtime returns the modification time of a file.
	Mtime(filename string) (time.Time, error)

	// Size returns the size of a file in bytes.
	Size(filename string) (int64, error)

	// Load reads a file's content, optionally at a specific revision.
	Load(filename string, revision string) (string, error)

	// LoadBytes reads a file's content as bytes, optionally at a specific revision.
	LoadBytes(filename string, revision string) ([]byte, error)

	// Store writes content to a file and commits it.
	Store(filename, content, message string, author Author) (bool, error)

	// StoreBytes writes binary content to a file and commits it.
	StoreBytes(filename string, content []byte, message string, author Author) (bool, error)

	// Delete removes a file or directory.
	Delete(filename string, message string, author Author) error

	// Rename renames a file.
	Rename(oldFilename, newFilename, message string, author Author) error

	// Metadata returns commit metadata for a file.
	Metadata(filename string, revision string) (*CommitMetadata, error)

	// Log returns the commit history for a file or the entire repository.
	Log(filename string, maxCount int) ([]CommitMetadata, error)

	// Blame returns blame information for a file.
	Blame(filename string, revision string) ([]BlameLine, error)

	// Diff returns the diff between two revisions.
	Diff(revA, revB string) (string, error)

	// ShowCommit returns metadata and diff for a specific commit.
	ShowCommit(revision string) (*CommitMetadata, string, error)

	// Revert reverts a commit.
	Revert(revision, message string, author Author) error

	// List returns files and directories in a path.
	List(path string, depth *int, exclude []string) (files, directories []string, err error)

	// Commit commits staged files.
	Commit(filenames []string, message string, author Author) error

	// GetParentRevision returns the parent revision for a file.
	GetParentRevision(filename, revision string) (string, error)

	// GetFilenameAtRevision returns the filename used at a specific revision.
	GetFilenameAtRevision(currentFilename, revision string) (string, error)
}
