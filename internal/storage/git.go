package storage

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var errIterDone = errors.New("iteration done")

// GitStorage implements Storage using a Git repository.
type GitStorage struct {
	path string
	repo *git.Repository
	mu   sync.Mutex
}

// NewGitStorage creates a new GitStorage for the given path.
func NewGitStorage(path string, initialize bool) (*GitStorage, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	var repo *git.Repository
	if initialize {
		repo, err = git.PlainInit(absPath, false)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize repository: %w", err)
		}
	} else {
		repo, err = git.PlainOpen(absPath)
		if err != nil {
			return nil, fmt.Errorf("no valid git repository in '%s': %w", absPath, err)
		}
	}

	return &GitStorage{
		path: absPath,
		repo: repo,
	}, nil
}

// Path returns the repository path.
func (g *GitStorage) Path() string {
	return g.path
}

// validatePath checks that the given path does not escape the repository root.
func (g *GitStorage) validatePath(filename string) error {
	if filename == "" {
		return nil
	}
	cleaned := filepath.Clean(filename)
	if filepath.IsAbs(cleaned) {
		return ErrPathTraversal
	}
	if strings.HasPrefix(cleaned, "..") {
		return ErrPathTraversal
	}
	joined := filepath.Join(g.path, cleaned)
	if !strings.HasPrefix(joined, g.path+string(filepath.Separator)) && joined != g.path {
		return ErrPathTraversal
	}
	return nil
}

// Exists checks if a file exists.
func (g *GitStorage) Exists(filename string) bool {
	if g.validatePath(filename) != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(g.path, filename))
	return err == nil
}

// IsDir checks if a path is a directory.
func (g *GitStorage) IsDir(dirname string) bool {
	if g.validatePath(dirname) != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(g.path, dirname))
	return err == nil && info.IsDir()
}

// IsEmptyDir checks if a directory is empty.
func (g *GitStorage) IsEmptyDir(dirname string) bool {
	if g.validatePath(dirname) != nil {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(g.path, dirname))
	return err == nil && len(entries) == 0
}

// Mtime returns the modification time of a file.
func (g *GitStorage) Mtime(filename string) (time.Time, error) {
	if err := g.validatePath(filename); err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(filepath.Join(g.path, filename))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// Size returns the size of a file in bytes.
func (g *GitStorage) Size(filename string) (int64, error) {
	if err := g.validatePath(filename); err != nil {
		return 0, err
	}
	info, err := os.Stat(filepath.Join(g.path, filename))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// checkReload checks if the repository needs to be reloaded.
func (g *GitStorage) checkReload() {
	reloadPath := filepath.Join(g.path, ".git", "RELOAD_GIT")
	if _, err := os.Stat(reloadPath); err == nil {
		if err := os.Remove(reloadPath); err != nil {
			slog.Warn("failed to remove reload marker", "error", err)
		}
		repo, err := git.PlainOpen(g.path)
		if err == nil {
			g.repo = repo
		}
	}
}

// Load reads a file's content.
func (g *GitStorage) Load(filename string, revision string) (string, error) {
	data, err := g.LoadBytes(filename, revision)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// LoadBytes reads a file's content as bytes.
func (g *GitStorage) LoadBytes(filename string, revision string) ([]byte, error) {
	if err := g.validatePath(filename); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkReload()

	if revision != "" {
		// Load from specific revision
		hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
		if err != nil {
			return nil, ErrNotFound
		}

		commit, err := g.repo.CommitObject(*hash)
		if err != nil {
			return nil, ErrNotFound
		}

		file, err := commit.File(filename)
		if err != nil {
			return nil, ErrNotFound
		}

		content, err := file.Contents()
		if err != nil {
			return nil, err
		}
		return []byte(content), nil
	}

	// Load from working directory
	data, err := os.ReadFile(filepath.Join(g.path, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

// Store writes content to a file and commits it.
func (g *GitStorage) Store(filename, content, message string, author Author) (bool, error) {
	return g.StoreBytes(filename, []byte(content), message, author)
}

// StoreBytes writes binary content to a file and commits it.
func (g *GitStorage) StoreBytes(filename string, content []byte, message string, author Author) (bool, error) {
	if err := g.validatePath(filename); err != nil {
		return false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if message == "" {
		message = "Update " + filename
	}

	fullPath := filepath.Join(g.path, filename)
	dir := filepath.Dir(fullPath)

	// Create directory if needed
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o775); err != nil {
			return false, err
		}
	}

	// Write file
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		return false, err
	}

	// Get worktree
	worktree, err := g.repo.Worktree()
	if err != nil {
		return false, err
	}

	// Check if file has changed
	status, err := worktree.Status()
	if err != nil {
		return false, err
	}

	fileStatus := status.File(filename)
	if fileStatus.Staging == git.Unmodified && fileStatus.Worktree == git.Unmodified {
		return false, nil
	}

	// Add and commit
	if _, err := worktree.Add(filename); err != nil {
		return false, err
	}

	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  author.Name,
			Email: author.Email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

// Delete removes a file or directory.
func (g *GitStorage) Delete(filename string, message string, author Author) error {
	if err := g.validatePath(filename); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	worktree, err := g.repo.Worktree()
	if err != nil {
		return err
	}

	// Check if it's a directory
	fullPath := filepath.Join(g.path, filename)
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if info.IsDir() {
		// For directories, we need to remove all files
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			// Empty directory, just remove it
			return os.Remove(fullPath)
		}
		// Remove recursively
		if _, err := worktree.Remove(filename); err != nil {
			return err
		}
	} else {
		if _, err := worktree.Remove(filename); err != nil {
			return err
		}
	}

	if message == "" {
		message = fmt.Sprintf("Deleted %s.", filename)
	}

	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  author.Name,
			Email: author.Email,
			When:  time.Now(),
		},
	})
	return err
}

// Rename renames a file.
func (g *GitStorage) Rename(oldFilename, newFilename, message string, author Author) error {
	if err := g.validatePath(oldFilename); err != nil {
		return err
	}
	if err := g.validatePath(newFilename); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Exists(newFilename) {
		return fmt.Errorf("the filename %q already exists", newFilename)
	}

	// Create target directory if needed
	newDir := filepath.Dir(filepath.Join(g.path, newFilename))
	if err := os.MkdirAll(newDir, 0o775); err != nil {
		return err
	}

	worktree, err := g.repo.Worktree()
	if err != nil {
		return err
	}

	// Move the file
	oldPath := filepath.Join(g.path, oldFilename)
	newPath := filepath.Join(g.path, newFilename)
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renaming %s to %s failed: %w", oldFilename, newFilename, err)
	}

	// Remove old file from index
	if _, err := worktree.Remove(oldFilename); err != nil {
		return err
	}

	// Add new file
	if _, err := worktree.Add(newFilename); err != nil {
		return err
	}

	if message == "" {
		message = fmt.Sprintf("%s renamed to %s.", oldFilename, newFilename)
	}

	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  author.Name,
			Email: author.Email,
			When:  time.Now(),
		},
	})
	return err
}

// Metadata returns commit metadata for a file.
func (g *GitStorage) Metadata(filename string, revision string) (*CommitMetadata, error) {
	if err := g.validatePath(filename); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkReload()

	var commit *object.Commit

	if revision == "" {
		// Get latest commit for file
		iter, err := g.repo.Log(&git.LogOptions{
			PathFilter: func(path string) bool {
				return path == filename
			},
		})
		if err != nil {
			return nil, ErrNotFound
		}
		defer iter.Close()

		commit, err = iter.Next()
		if err != nil {
			return nil, ErrNotFound
		}
	} else {
		// Get specific commit
		hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
		if err != nil {
			return nil, ErrNotFound
		}

		commit, err = g.repo.CommitObject(*hash)
		if err != nil {
			return nil, ErrNotFound
		}
	}

	return g.commitToMetadata(commit, false)
}

func (g *GitStorage) commitToMetadata(commit *object.Commit, includeFiles bool) (*CommitMetadata, error) {
	var files []string

	if includeFiles {
		// Get parent to compute diff
		parentIter := commit.Parents()
		parent, err := parentIter.Next()
		parentIter.Close()

		if err == nil {
			// Get changed files between parent and this commit
			parentTree, err := parent.Tree()
			if err != nil {
				slog.Warn("failed to load parent tree", "commit", commit.Hash.String()[:6], "error", err)
				parentTree = nil
			}
			commitTree, err := commit.Tree()
			if err != nil {
				slog.Warn("failed to load commit tree", "commit", commit.Hash.String()[:6], "error", err)
				commitTree = nil
			}
			if parentTree != nil && commitTree != nil {
				changes, err := parentTree.Diff(commitTree)
				if err == nil {
					for _, change := range changes {
						if change.From.Name != "" {
							files = append(files, change.From.Name)
						} else if change.To.Name != "" {
							files = append(files, change.To.Name)
						}
					}
				}
			}
		} else {
			// Initial commit - list all files
			tree, err := commit.Tree()
			if err != nil {
				slog.Warn("failed to load tree for initial commit", "commit", commit.Hash.String()[:6], "error", err)
			}
			if err == nil {
				tree.Files().ForEach(func(f *object.File) error {
					files = append(files, f.Name)
					return nil
				})
			}
		}
	}

	return &CommitMetadata{
		Revision:     commit.Hash.String()[:6],
		RevisionFull: commit.Hash.String(),
		Datetime:     commit.Author.When,
		AuthorName:   commit.Author.Name,
		AuthorEmail:  commit.Author.Email,
		Message:      strings.TrimSpace(commit.Message),
		Files:        files,
	}, nil
}

// Log returns the commit history.
func (g *GitStorage) Log(filename string, maxCount int) ([]CommitMetadata, error) {
	if filename != "" {
		if err := g.validatePath(filename); err != nil {
			return nil, err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.logLocked(filename, maxCount)
}

// logLocked performs the log operation. Caller must hold g.mu.
func (g *GitStorage) logLocked(filename string, maxCount int) ([]CommitMetadata, error) {
	g.checkReload()

	opts := &git.LogOptions{
		Order: git.LogOrderCommitterTime,
	}

	if filename != "" {
		opts.PathFilter = func(path string) bool {
			return path == filename
		}
	}

	iter, err := g.repo.Log(opts)
	if err != nil {
		return nil, ErrNotFound
	}
	defer iter.Close()

	var result []CommitMetadata
	count := 0

	err = iter.ForEach(func(commit *object.Commit) error {
		if maxCount > 0 && count >= maxCount {
			return errIterDone
		}

		meta, err := g.commitToMetadata(commit, false)
		if err != nil {
			return err
		}
		result = append(result, *meta)
		count++
		return nil
	})

	if err != nil && !errors.Is(err, errIterDone) && len(result) == 0 {
		return nil, ErrNotFound
	}

	return result, nil
}

// Blame returns blame information for a file.
func (g *GitStorage) Blame(filename string, revision string) ([]BlameLine, error) {
	if err := g.validatePath(filename); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkReload()

	var commitHash plumbing.Hash
	if revision == "" {
		head, err := g.repo.Head()
		if err != nil {
			return nil, ErrNotFound
		}
		commitHash = head.Hash()
	} else {
		hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
		if err != nil {
			return nil, ErrNotFound
		}
		commitHash = *hash
	}

	commit, err := g.repo.CommitObject(commitHash)
	if err != nil {
		return nil, ErrNotFound
	}

	result, err := git.Blame(commit, filename)
	if err != nil {
		return nil, ErrNotFound
	}

	var lines []BlameLine
	for i, line := range result.Lines {
		lines = append(lines, BlameLine{
			Revision:   line.Hash.String()[:6],
			AuthorName: line.Author,
			Datetime:   line.Date,
			LineNumber: i + 1,
			Line:       line.Text,
			Message:    "",
		})
	}

	return lines, nil
}

// Diff returns the diff between two revisions.
func (g *GitStorage) Diff(revA, revB string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	hashA, err := g.repo.ResolveRevision(plumbing.Revision(revA))
	if err != nil {
		return "", ErrNotFound
	}

	hashB, err := g.repo.ResolveRevision(plumbing.Revision(revB))
	if err != nil {
		return "", ErrNotFound
	}

	commitA, err := g.repo.CommitObject(*hashA)
	if err != nil {
		return "", err
	}

	commitB, err := g.repo.CommitObject(*hashB)
	if err != nil {
		return "", err
	}

	treeA, err := commitA.Tree()
	if err != nil {
		return "", err
	}

	treeB, err := commitB.Tree()
	if err != nil {
		return "", err
	}

	changes, err := treeA.Diff(treeB)
	if err != nil {
		return "", err
	}

	patch, err := changes.Patch()
	if err != nil {
		return "", err
	}

	return patch.String(), nil
}

// ShowCommit returns metadata and diff for a specific commit.
func (g *GitStorage) ShowCommit(revision string) (*CommitMetadata, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return nil, "", fmt.Errorf("no commit found for ref %s", revision)
	}

	commit, err := g.repo.CommitObject(*hash)
	if err != nil {
		return nil, "", err
	}

	meta, err := g.commitToMetadata(commit, true)
	if err != nil {
		return nil, "", err
	}

	// Get diff with parent
	var diff string
	parentIter := commit.Parents()
	parent, err := parentIter.Next()
	parentIter.Close()

	if err == nil {
		parentTree, err := parent.Tree()
		if err != nil {
			slog.Warn("failed to load parent tree", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		commitTree, err := commit.Tree()
		if err != nil {
			slog.Warn("failed to load commit tree", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		changes, err := parentTree.Diff(commitTree)
		if err != nil {
			slog.Warn("failed to diff trees", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		patch, err := changes.Patch()
		if err != nil {
			slog.Warn("failed to generate patch", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		diff = patch.String()
	} else {
		// Initial commit - show all files as added
		tree, err := commit.Tree()
		if err != nil {
			slog.Warn("failed to load tree for initial commit", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		changes, err := object.DiffTree(nil, tree)
		if err != nil {
			slog.Warn("failed to diff tree for initial commit", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		patch, err := changes.Patch()
		if err != nil {
			slog.Warn("failed to generate patch for initial commit", "commit", commit.Hash.String()[:6], "error", err)
			return meta, "", nil
		}
		diff = patch.String()
	}

	return meta, diff, nil
}

// Revert reverts a commit.
func (g *GitStorage) Revert(revision, message string, author Author) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return fmt.Errorf("commit not found: %w", err)
	}

	commit, err := g.repo.CommitObject(*hash)
	if err != nil {
		return err
	}

	// Get parent commit
	parentIter := commit.Parents()
	parent, err := parentIter.Next()
	parentIter.Close()
	if err != nil {
		return fmt.Errorf("cannot revert initial commit")
	}

	// Get the files that changed in this commit
	parentTree, err := parent.Tree()
	if err != nil {
		return err
	}

	commitTree, err := commit.Tree()
	if err != nil {
		return err
	}

	changes, err := parentTree.Diff(commitTree)
	if err != nil {
		return err
	}

	worktree, err := g.repo.Worktree()
	if err != nil {
		return err
	}

	// Restore each changed file to its parent state
	for _, change := range changes {
		if change.From.Name != "" {
			// File was modified or deleted - restore from parent
			file, err := parentTree.File(change.From.Name)
			if err == nil {
				content, err := file.Contents()
				if err != nil {
					return fmt.Errorf("failed to read file %s from parent: %w", change.From.Name, err)
				}
				fullPath := filepath.Join(g.path, change.From.Name)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0o775); err != nil {
					return fmt.Errorf("failed to create directory for %s: %w", change.From.Name, err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
					return fmt.Errorf("failed to write file %s: %w", change.From.Name, err)
				}
				if _, err := worktree.Add(change.From.Name); err != nil {
				return fmt.Errorf("failed to stage restored file %s: %w", change.From.Name, err)
			}
			}
		}
		if change.To.Name != "" && change.From.Name == "" {
			// File was added - remove it
			if _, err := worktree.Remove(change.To.Name); err != nil {
			return fmt.Errorf("failed to stage removal of %s: %w", change.To.Name, err)
		}
		}
	}

	if message == "" {
		message = fmt.Sprintf("Revert %q", commit.Message)
	}

	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  author.Name,
			Email: author.Email,
			When:  time.Now(),
		},
	})

	return err
}

// List returns files and directories in a path.
func (g *GitStorage) List(p string, depth *int, exclude []string) (files, directories []string, err error) {
	if p != "" {
		if err := g.validatePath(p); err != nil {
			return nil, nil, err
		}
	}
	excludeSet := make(map[string]bool)
	excludeSet[".git"] = true
	for _, e := range exclude {
		excludeSet[e] = true
	}

	var fullPath string
	if p != "" {
		if filepath.IsAbs(p) {
			return nil, nil, fmt.Errorf("path must not be absolute")
		}
		fullPath = filepath.Join(g.path, p)
	} else {
		fullPath = g.path
	}

	err = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Get relative path
		relPath, err := filepath.Rel(fullPath, path)
		if err != nil {
			slog.Warn("failed to compute relative path", "path", path, "error", err)
			return nil
		}
		if relPath == "." {
			return nil
		}

		// Check exclusions
		parts := strings.Split(relPath, string(filepath.Separator))
		for _, part := range parts {
			if excludeSet[part] {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Check depth
		if depth != nil && len(parts) > *depth+1 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			directories = append(directories, relPath)
		} else {
			files = append(files, relPath)
		}

		return nil
	})

	sort.Strings(files)
	sort.Strings(directories)

	return files, directories, err
}

// Commit commits staged files.
func (g *GitStorage) Commit(filenames []string, message string, author Author) error {
	for _, filename := range filenames {
		if err := g.validatePath(filename); err != nil {
			return err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	worktree, err := g.repo.Worktree()
	if err != nil {
		return err
	}

	for _, filename := range filenames {
		if _, err := worktree.Add(filename); err != nil {
			return fmt.Errorf("failed to add %s: %w", filename, err)
		}
	}

	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  author.Name,
			Email: author.Email,
			When:  time.Now(),
		},
	})
	return err
}

// GetParentRevision returns the parent revision for a file.
func (g *GitStorage) GetParentRevision(filename, revision string) (string, error) {
	if err := g.validatePath(filename); err != nil {
		return "", err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	history, err := g.logLocked(filename, 0)
	if err != nil {
		return "", err
	}

	for i, entry := range history {
		if entry.Revision == revision || strings.HasPrefix(entry.RevisionFull, revision) {
			if i+1 < len(history) {
				return history[i+1].Revision, nil
			}
			return "", ErrNotFound
		}
	}
	return "", ErrNotFound
}

// GetFilenameAtRevision returns the filename used at a specific revision.
func (g *GitStorage) GetFilenameAtRevision(currentFilename, revision string) (string, error) {
	if err := g.validatePath(currentFilename); err != nil {
		return "", err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	hash, err := g.repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return currentFilename, nil
	}

	commit, err := g.repo.CommitObject(*hash)
	if err != nil {
		return currentFilename, nil
	}

	tree, err := commit.Tree()
	if err != nil {
		return currentFilename, nil
	}

	// Check if file exists at this revision
	_, err = tree.File(currentFilename)
	if err == nil {
		return currentFilename, nil
	}

	// File doesn't exist - try to find it with rename tracking
	// This is a simplified implementation
	return currentFilename, nil
}

var _ Storage = (*GitStorage)(nil)
