package quarto

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sa/gopherwiki/internal/rendercache"
)

// fakeRunner emulates `quarto render` by writing canned HTML to the --output
// path, capturing the args and working dir it was invoked with.
type fakeRunner struct {
	html     string
	err      error
	gotArgs  []string
	gotDir   string
	gotName  string
	sawSource string
}

func (f *fakeRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	f.gotDir = dir
	f.gotName = name
	f.gotArgs = args
	// Record the source that the renderer wrote.
	if b, err := os.ReadFile(filepath.Join(dir, "index.qmd")); err == nil {
		f.sawSource = string(b)
	}
	if f.err != nil {
		return nil, []byte("boom"), f.err
	}
	// Emulate quarto producing the --output file.
	out := outputArg(args)
	if out != "" {
		_ = os.WriteFile(filepath.Join(dir, out), []byte(f.html), 0o600)
	}
	return []byte("rendered"), nil, nil
}

func outputArg(args []string) string {
	for i, a := range args {
		if a == "--output" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func availableCaps() Capabilities {
	return Capabilities{Available: true, Version: "1.9.37", Path: "/usr/local/bin/quarto"}
}

func TestRenderHTMLWritesSourceAndReadsOutput(t *testing.T) {
	fr := &fakeRunner{html: "<html>OK</html>"}
	r := &Renderer{caps: availableCaps(), runner: fr, timeout: time.Second}

	html, err := r.RenderHTML(context.Background(), Input{Source: "---\ntitle: T\n---\n# H\n"})
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if string(html) != "<html>OK</html>" {
		t.Errorf("html = %q", html)
	}
	if fr.sawSource != "---\ntitle: T\n---\n# H\n" {
		t.Errorf("source written = %q", fr.sawSource)
	}
	if fr.gotName != "/usr/local/bin/quarto" {
		t.Errorf("command = %q, want quarto path", fr.gotName)
	}
	joined := strings.Join(fr.gotArgs, " ")
	for _, want := range []string{"render", "index.qmd", "--to html", "--embed-resources", "--output index.html"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestRenderHTMLCleansUpWorkDir(t *testing.T) {
	fr := &fakeRunner{html: "x"}
	r := &Renderer{caps: availableCaps(), runner: fr, timeout: time.Second}
	if _, err := r.RenderHTML(context.Background(), Input{Source: "s"}); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if _, err := os.Stat(fr.gotDir); !os.IsNotExist(err) {
		t.Errorf("work dir %q should be removed, stat err = %v", fr.gotDir, err)
	}
}

func TestRenderHTMLRunnerErrorSurfaced(t *testing.T) {
	fr := &fakeRunner{err: errors.New("exit 1")}
	r := &Renderer{caps: availableCaps(), runner: fr, timeout: time.Second}
	_, err := r.RenderHTML(context.Background(), Input{Source: "s"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "render failed") {
		t.Errorf("error = %v, want render failed", err)
	}
}

func TestRenderHTMLUnavailable(t *testing.T) {
	r := &Renderer{caps: Capabilities{Available: false}, runner: &fakeRunner{}, timeout: time.Second}
	if _, err := r.RenderHTML(context.Background(), Input{Source: "s"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

// fakeHTMLRenderer is an htmlRenderer for exercising Service orchestration
// without touching the filesystem.
type fakeHTMLRenderer struct {
	html     string
	err      error
	calls    int32
	inflight int32
	maxSeen  int32
	delay    time.Duration
}

func (f *fakeHTMLRenderer) RenderHTML(ctx context.Context, in Input) ([]byte, error) {
	cur := atomic.AddInt32(&f.inflight, 1)
	for {
		old := atomic.LoadInt32(&f.maxSeen)
		if cur <= old || atomic.CompareAndSwapInt32(&f.maxSeen, old, cur) {
			break
		}
	}
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	atomic.AddInt32(&f.inflight, -1)
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.html + ":" + in.Pagepath), nil
}

func newServiceWithFake(t *testing.T, fr *fakeHTMLRenderer, concurrency int) (*Service, *rendercache.Cache) {
	t.Helper()
	cache, err := rendercache.Open(":memory:")
	if err != nil {
		t.Fatalf("cache open: %v", err)
	}
	t.Cleanup(func() { cache.Close() })
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	s := &Service{
		caps:        availableCaps(),
		renderer:    fr,
		cache:       cache,
		sem:         make(chan struct{}, concurrency),
		fingerprint: availableCaps().Fingerprint(),
	}
	return s, cache
}

func TestServiceRenderStoresInCache(t *testing.T) {
	ctx := context.Background()
	fr := &fakeHTMLRenderer{html: "<h1>out</h1>"}
	s, cache := newServiceWithFake(t, fr, 2)

	in := Input{Pagepath: "analysis", Source: "src", Engine: "jupyter", SourceRevision: "rev1"}
	entry, err := s.Render(ctx, in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(entry.HTML), "out") {
		t.Errorf("entry HTML = %q", entry.HTML)
	}

	// Retrievable via the content key.
	got, ok, err := cache.Get(ctx, s.CachedKey("src", "jupyter"))
	if err != nil || !ok {
		t.Fatalf("cache lookup: ok=%v err=%v", ok, err)
	}
	if got.Pagepath != "analysis" || got.SourceRevision != "rev1" || got.Engine != "jupyter" {
		t.Errorf("stored metadata mismatch: %+v", got)
	}
}

func TestServiceCachedRoundtrip(t *testing.T) {
	ctx := context.Background()
	fr := &fakeHTMLRenderer{html: "cached"}
	s, _ := newServiceWithFake(t, fr, 1)

	if _, ok, _ := s.Cached(ctx, "src", "jupyter"); ok {
		t.Fatal("expected miss before render")
	}
	if _, err := s.Render(ctx, Input{Pagepath: "p", Source: "src", Engine: "jupyter"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, ok, err := s.Cached(ctx, "src", "jupyter"); err != nil || !ok {
		t.Fatalf("expected hit after render: ok=%v err=%v", ok, err)
	}
}

func TestServiceConcurrencyCap(t *testing.T) {
	ctx := context.Background()
	fr := &fakeHTMLRenderer{html: "x", delay: 25 * time.Millisecond}
	s, _ := newServiceWithFake(t, fr, 2)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct sources so cache writes do not collide meaningfully.
			_, _ = s.Render(ctx, Input{Pagepath: "p", Source: string(rune('a' + i))})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&fr.maxSeen); got > 2 {
		t.Errorf("max concurrent renders = %d, want <= 2 (semaphore cap)", got)
	}
	if got := atomic.LoadInt32(&fr.calls); got != 8 {
		t.Errorf("calls = %d, want 8", got)
	}
}

func TestServiceUnavailableWithoutCache(t *testing.T) {
	s := NewService(availableCaps(), nil, time.Second, 1)
	if s.Available() {
		t.Error("service with nil cache should be unavailable")
	}
	if _, err := s.Render(context.Background(), Input{Source: "s"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Render err = %v, want ErrUnavailable", err)
	}
}

func TestServiceUnavailableWithoutQuarto(t *testing.T) {
	cache, _ := rendercache.Open(":memory:")
	defer cache.Close()
	s := NewService(Capabilities{Available: false}, cache, time.Second, 1)
	if s.Available() {
		t.Error("service without quarto should be unavailable")
	}
}

func TestServiceInvalidate(t *testing.T) {
	ctx := context.Background()
	fr := &fakeHTMLRenderer{html: "x"}
	s, _ := newServiceWithFake(t, fr, 1)

	s.Render(ctx, Input{Pagepath: "p", Source: "s1", Engine: ""})
	s.Render(ctx, Input{Pagepath: "p", Source: "s2", Engine: ""})
	if err := s.Invalidate(ctx, "p"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if _, ok, _ := s.Cached(ctx, "s1", ""); ok {
		t.Error("expected page renders invalidated")
	}
}

// --- Gated integration test: only runs when a real Quarto is installed. ---

func TestIntegrationRealQuartoRender(t *testing.T) {
	caps := Detect(context.Background(), "")
	if !caps.Available {
		t.Skip("quarto not installed; skipping real render integration test")
	}

	cache, err := rendercache.Open(":memory:")
	if err != nil {
		t.Fatalf("cache open: %v", err)
	}
	defer cache.Close()

	s := NewService(caps, cache, 90*time.Second, 1)
	// A plain markdown doc (no code execution) keeps the test fast and free of
	// language-runtime dependencies while exercising the full render+cache path.
	src := "---\ntitle: Integration\n---\n\n# Hello\n\nSome **bold** text.\n"
	entry, err := s.Render(context.Background(), Input{Pagepath: "itest", Source: src, SourceRevision: "rev"})
	if err != nil {
		t.Fatalf("real render: %v", err)
	}
	if !strings.Contains(string(entry.HTML), "Hello") {
		t.Errorf("rendered HTML missing heading; got %d bytes", len(entry.HTML))
	}
	// Self-contained output should be a full HTML document.
	if !strings.Contains(strings.ToLower(string(entry.HTML)), "<!doctype html") {
		t.Errorf("expected a standalone HTML document")
	}
	if _, ok, _ := s.Cached(context.Background(), src, ""); !ok {
		t.Error("expected render to be cached")
	}
}
