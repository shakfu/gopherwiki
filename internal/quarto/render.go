package quarto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sa/gopherwiki/internal/rendercache"
)

// ErrUnavailable is returned when a render is requested but the Quarto toolchain
// or render cache is not configured.
var ErrUnavailable = errors.New("quarto: rendering unavailable")

// Default render tuning.
const (
	defaultTimeout     = 120 * time.Second
	defaultConcurrency = 2
)

// Input describes one page to render.
type Input struct {
	// Pagepath is the wiki path of the page (used for cache invalidation).
	Pagepath string
	// Source is the raw page source, including frontmatter. Quarto reads the
	// engine and execute options from the frontmatter directly.
	Source string
	// Engine is the resolved render engine ("knitr", "jupyter", or ""). It is
	// used only as a cache-key input; Quarto itself resolves the engine from the
	// source frontmatter/cells.
	Engine string
	// SourceRevision is the git revision the source came from, stored alongside
	// the cached output.
	SourceRevision string
}

// Runner executes an external command in a working directory. It is the seam
// that lets tests substitute the real quarto invocation.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error)
}

// execRunner runs commands via os/exec.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// Do not inherit the parent environment's secrets. Rendering runs author
	// code with the author's trust, not the server's; keep application secrets
	// (session keys, DB credentials) out of its environment. A minimal PATH and
	// HOME are retained so the toolchain can locate itself and its caches.
	cmd.Env = renderEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// renderEnv returns a minimal environment for the render subprocess. It
// deliberately excludes the parent process environment so application secrets do
// not leak into author-controlled code, while retaining the few variables the
// toolchain needs to locate itself and its caches.
func renderEnv() []string {
	var env []string
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// htmlRenderer produces self-contained HTML from a page source. *Renderer is the
// production implementation; tests supply a fake.
type htmlRenderer interface {
	RenderHTML(ctx context.Context, in Input) ([]byte, error)
}

// Renderer invokes Quarto to render a page source to a single self-contained
// HTML document in an isolated temporary directory.
type Renderer struct {
	caps     Capabilities
	runner   Runner
	timeout  time.Duration
	workRoot string // parent dir for temp work dirs ("" = os default)
}

// NewRenderer builds a Renderer using the real exec runner.
func NewRenderer(caps Capabilities, timeout time.Duration) *Renderer {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Renderer{caps: caps, runner: execRunner{}, timeout: timeout}
}

// RenderHTML writes the source into a fresh temp directory, runs
// `quarto render ... --to html --embed-resources`, and returns the rendered
// bytes. The temp directory (including any executed outputs) is always removed.
func (r *Renderer) RenderHTML(ctx context.Context, in Input) ([]byte, error) {
	if !r.caps.Available {
		return nil, ErrUnavailable
	}

	dir, err := os.MkdirTemp(r.workRoot, "gopherwiki-render-*")
	if err != nil {
		return nil, fmt.Errorf("quarto: create work dir: %w", err)
	}
	defer os.RemoveAll(dir)

	const srcName = "index.qmd"
	const outName = "index.html"
	if err := os.WriteFile(filepath.Join(dir, srcName), []byte(in.Source), 0o600); err != nil {
		return nil, fmt.Errorf("quarto: write source: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []string{"render", srcName, "--to", "html", "--embed-resources", "--output", outName}
	_, stderr, err := r.runner.Run(ctx, dir, r.caps.Path, args...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("quarto: render timed out after %s", r.timeout)
		}
		return nil, fmt.Errorf("quarto: render failed: %w: %s", err, string(stderr))
	}

	html, err := os.ReadFile(filepath.Join(dir, outName))
	if err != nil {
		return nil, fmt.Errorf("quarto: read output: %w", err)
	}
	return html, nil
}

// Service is the gated render orchestrator. It bounds concurrency, renders a
// page's source to HTML, and stores the result in the render cache keyed by
// content. It is safe for concurrent use.
type Service struct {
	caps        Capabilities
	renderer    htmlRenderer
	cache       *rendercache.Cache
	sem         chan struct{}
	fingerprint string
}

// NewService constructs a Service. concurrency <= 0 uses the default. A nil
// cache or unavailable capabilities makes the service report Available() ==
// false and reject renders with ErrUnavailable.
func NewService(caps Capabilities, cache *rendercache.Cache, timeout time.Duration, concurrency int) *Service {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	return &Service{
		caps:        caps,
		renderer:    NewRenderer(caps, timeout),
		cache:       cache,
		sem:         make(chan struct{}, concurrency),
		fingerprint: caps.Fingerprint(),
	}
}

// Available reports whether renders can be performed and cached.
func (s *Service) Available() bool {
	return s.caps.Available && s.cache != nil
}

// CachedKey returns the cache key a page's current source maps to.
func (s *Service) CachedKey(source, engine string) string {
	return rendercache.Key(source, engine, s.fingerprint)
}

// Cached returns the stored render for a page's current source, if present. A
// nil cache yields (zero, false, nil).
func (s *Service) Cached(ctx context.Context, source, engine string) (rendercache.Entry, bool, error) {
	if s.cache == nil {
		return rendercache.Entry{}, false, nil
	}
	return s.cache.Get(ctx, s.CachedKey(source, engine))
}

// Render executes a page and stores its output in the cache, returning the
// stored entry. Concurrency is bounded by the service's semaphore; the call
// blocks for a slot or until ctx is cancelled.
func (s *Service) Render(ctx context.Context, in Input) (rendercache.Entry, error) {
	if !s.Available() {
		return rendercache.Entry{}, ErrUnavailable
	}

	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return rendercache.Entry{}, ctx.Err()
	}

	html, err := s.renderer.RenderHTML(ctx, in)
	if err != nil {
		return rendercache.Entry{}, err
	}

	entry := rendercache.Entry{
		Key:            s.CachedKey(in.Source, in.Engine),
		Pagepath:       in.Pagepath,
		SourceRevision: in.SourceRevision,
		Engine:         in.Engine,
		HTML:           html,
	}
	if err := s.cache.Put(ctx, entry); err != nil {
		return rendercache.Entry{}, err
	}
	return entry, nil
}

// Invalidate removes any cached renders for a page (e.g. after its source is
// edited or the page is deleted).
func (s *Service) Invalidate(ctx context.Context, pagepath string) error {
	if s.cache == nil {
		return nil
	}
	_, err := s.cache.DeleteByPage(ctx, pagepath)
	return err
}
