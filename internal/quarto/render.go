package quarto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
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

// Interpreters optionally pins the Python and R executables Quarto uses at
// render time. Empty fields fall back to Quarto's own discovery (PATH, an active
// virtualenv/conda env, or the frontmatter kernelspec). They are surfaced to the
// render subprocess as the QUARTO_PYTHON and QUARTO_R environment variables,
// which Quarto reads to select an interpreter -- useful when the desired Python
// (with its Jupyter machinery) or R (with knitr) is not on the render host's
// PATH. See docs/computational-pages.md.
type Interpreters struct {
	Python string // -> QUARTO_PYTHON
	R      string // -> QUARTO_R
}

// execRunner runs commands via os/exec.
type execRunner struct {
	interp Interpreters
}

func (e execRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// Do not inherit the parent environment's secrets. Rendering runs author
	// code with the author's trust, not the server's; keep application secrets
	// (session keys, DB credentials) out of its environment. A minimal PATH and
	// HOME are retained so the toolchain can locate itself and its caches.
	cmd.Env = renderEnv(e.interp)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// renderEnvAllowlist is the set of parent-process variables forwarded to the
// render subprocess. It excludes application secrets by construction (only these
// names pass) while retaining what the toolchain needs to locate itself and its
// caches, plus Quarto's own interpreter-selection variables so an operator can
// pin Python/R by setting them in the server environment.
var renderEnvAllowlist = []string{
	"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL",
	"QUARTO_PYTHON", "QUARTO_R", "QUARTO_R_HOME",
}

// renderEnv returns the minimal, deterministic environment for the render
// subprocess: the allowlisted parent variables, with explicit interpreter
// overrides taking precedence over any ambient QUARTO_PYTHON/QUARTO_R.
func renderEnv(interp Interpreters) []string {
	vals := make(map[string]string, len(renderEnvAllowlist)+2)
	for _, key := range renderEnvAllowlist {
		if v, ok := os.LookupEnv(key); ok {
			vals[key] = v
		}
	}
	// Explicit configuration wins over whatever was inherited.
	if interp.Python != "" {
		vals["QUARTO_PYTHON"] = interp.Python
	}
	if interp.R != "" {
		vals["QUARTO_R"] = interp.R
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+vals[k])
	}
	return env
}

// pageRenderer renders a page source: to self-contained HTML for the gated
// computational render, or to an arbitrary export format. *Renderer is the
// production implementation; tests supply a fake.
type pageRenderer interface {
	RenderHTML(ctx context.Context, in Input) ([]byte, error)
	RenderTo(ctx context.Context, in Input, f ExportFormat) ([]byte, error)
}

// Renderer invokes Quarto to render a page source to a single self-contained
// HTML document in an isolated temporary directory.
type Renderer struct {
	caps        Capabilities
	runner      Runner
	timeout     time.Duration
	workRoot    string // parent dir for temp work dirs ("" = os default)
	ojsLibsBase string // when set, OJS CDN URLs are rewritten to this local base

	// ojsOnce guards a one-time load of the OJS runtime bundle (caps.OJSRuntimePath)
	// into ojsRuntime, used to make Observable JS cells executable in the
	// self-contained render (Quarto's --embed-resources does not inline it).
	ojsOnce    sync.Once
	ojsRuntime []byte
}

// NewRenderer builds a Renderer using the real exec runner. interp optionally
// pins the Python/R interpreters Quarto uses; ojsLibsBase, when non-empty,
// rewrites OJS CDN URLs to that local base so libraries load from the wiki.
func NewRenderer(caps Capabilities, timeout time.Duration, interp Interpreters, ojsLibsBase string) *Renderer {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Renderer{caps: caps, runner: execRunner{interp: interp}, timeout: timeout, ojsLibsBase: ojsLibsBase}
}

// RenderHTML writes the source into a fresh temp directory, runs
// `quarto render ... --to html --embed-resources`, and returns the rendered
// bytes. This is the gated computational render: code cells execute. The temp
// directory (including any executed outputs) is always removed. The OJS runtime
// is injected into the result so Observable JS cells execute (see injectOJSRuntime).
func (r *Renderer) RenderHTML(ctx context.Context, in Input) ([]byte, error) {
	out, err := r.renderToFile(ctx, in.Source, []string{"--to", "html", "--embed-resources"}, "index.html")
	if err != nil {
		return nil, err
	}
	out = injectOJSRuntime(out, r.ojsRuntimeBundle())
	out = rewriteOJSCDNs(out, r.ojsLibsBase)
	return out, nil
}

// ojsRuntimeBundle lazily loads and caches Quarto's OJS runtime bundle. It
// returns nil when the runtime path is unknown or unreadable, in which case OJS
// injection is skipped (cells render as inert source; the rest of the page is
// unaffected).
func (r *Renderer) ojsRuntimeBundle() []byte {
	r.ojsOnce.Do(func() {
		if r.caps.OJSRuntimePath == "" {
			return
		}
		if b, err := os.ReadFile(r.caps.OJSRuntimePath); err == nil {
			r.ojsRuntime = b
		}
	})
	return r.ojsRuntime
}

// RenderTo renders a page source to the given export format and returns the
// output bytes. Execution is always disabled (--no-execute): export must not be
// a backdoor around the gated-execution rule, and plain pages have nothing to
// execute anyway. See docs/computational-pages.md Section 6.
func (r *Renderer) RenderTo(ctx context.Context, in Input, f ExportFormat) ([]byte, error) {
	args := []string{"--to", f.To, "--no-execute"}
	if f.EmbedResources {
		args = append(args, "--embed-resources")
	}
	return r.renderToFile(ctx, in.Source, args, "index."+f.Ext)
}

// renderToFile writes source into a fresh temp directory, runs `quarto render`
// with the given extra args producing outName, and returns that file's bytes.
// The temp directory (including any executed outputs) is always removed.
func (r *Renderer) renderToFile(ctx context.Context, source string, extraArgs []string, outName string) ([]byte, error) {
	if !r.caps.Available {
		return nil, ErrUnavailable
	}

	dir, err := os.MkdirTemp(r.workRoot, "gopherwiki-render-*")
	if err != nil {
		return nil, fmt.Errorf("quarto: create work dir: %w", err)
	}
	defer os.RemoveAll(dir)

	const srcName = "index.qmd"
	if err := os.WriteFile(filepath.Join(dir, srcName), []byte(source), 0o600); err != nil {
		return nil, fmt.Errorf("quarto: write source: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := append([]string{"render", srcName}, extraArgs...)
	args = append(args, "--output", outName)
	_, stderr, err := r.runner.Run(ctx, dir, r.caps.Path, args...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("quarto: render timed out after %s", r.timeout)
		}
		return nil, fmt.Errorf("quarto: render failed: %w: %s", err, string(stderr))
	}

	out, err := os.ReadFile(filepath.Join(dir, outName))
	if err != nil {
		return nil, fmt.Errorf("quarto: read output: %w", err)
	}
	return out, nil
}

// Service is the gated render orchestrator. It bounds concurrency, renders a
// page's source to HTML, and stores the result in the render cache keyed by
// content. It is safe for concurrent use.
type Service struct {
	caps          Capabilities
	renderer      pageRenderer
	cache         *rendercache.Cache
	sem           chan struct{}
	fingerprint   string
	exportEnabled bool
}

// Option configures a Service at construction time.
type Option func(*serviceOptions)

// serviceOptions holds the tunables an Option can set.
type serviceOptions struct {
	interp        Interpreters
	exportEnabled bool
	ojsLibsBase   string
}

// WithInterpreters pins the Python and/or R executables Quarto uses at render
// time. Empty strings leave Quarto's own discovery in place.
func WithInterpreters(interp Interpreters) Option {
	return func(o *serviceOptions) { o.interp = interp }
}

// WithExport enables Quarto-produced page export (PDF/HTML/DOCX/EPUB/GFM). It is
// off by default so that merely having the toolchain present does not expose
// export endpoints; the operator opts in explicitly.
func WithExport(enabled bool) Option {
	return func(o *serviceOptions) { o.exportEnabled = enabled }
}

// WithOJSLocalLibs makes rendered OJS pages load their Observable/jsDelivr
// libraries from base (a local URL path the wiki serves the mirror at, e.g.
// "/ojs-libs") instead of the external CDNs, for offline/air-gapped operation.
// Empty leaves the CDN default in place.
func WithOJSLocalLibs(base string) Option {
	return func(o *serviceOptions) { o.ojsLibsBase = base }
}

// NewService constructs a Service. concurrency <= 0 uses the default. A nil
// cache or unavailable capabilities makes the service report Available() ==
// false and reject renders with ErrUnavailable.
func NewService(caps Capabilities, cache *rendercache.Cache, timeout time.Duration, concurrency int, opts ...Option) *Service {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	var o serviceOptions
	for _, opt := range opts {
		opt(&o)
	}
	// Local-libs mode changes the rendered URLs, so fold it into the cache
	// fingerprint to invalidate output produced in the other mode.
	fingerprint := caps.Fingerprint()
	if o.ojsLibsBase != "" {
		fingerprint += "/ojslocal"
	}
	return &Service{
		caps:          caps,
		renderer:      NewRenderer(caps, timeout, o.interp, o.ojsLibsBase),
		cache:         cache,
		sem:           make(chan struct{}, concurrency),
		fingerprint:   fingerprint,
		exportEnabled: o.exportEnabled,
	}
}

// Available reports whether gated renders (code execution) can be performed and
// cached. This requires both a usable toolchain and a render cache.
func (s *Service) Available() bool {
	return s.caps.Available && s.cache != nil
}

// ExportAvailable reports whether Quarto-produced exports can be generated. This
// requires a usable toolchain and that export was explicitly enabled; it does
// not require the render cache (export runs with --no-execute and does not touch
// the cache), so it is decoupled from gated execution.
func (s *Service) ExportAvailable() bool {
	return s.caps.Available && s.exportEnabled
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
