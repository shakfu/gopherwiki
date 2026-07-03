package rendercache

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPutGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)

	in := Entry{
		Key:            "k1",
		Pagepath:       "analysis",
		SourceRevision: "abc123",
		Engine:         "jupyter",
		HTML:           []byte("<html>output</html>"),
	}
	if err := c.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := c.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	if string(got.HTML) != "<html>output</html>" {
		t.Errorf("HTML = %q", got.HTML)
	}
	if got.Pagepath != "analysis" || got.SourceRevision != "abc123" || got.Engine != "jupyter" {
		t.Errorf("metadata mismatch: %+v", got)
	}
	if got.Size != int64(len(in.HTML)) {
		t.Errorf("Size = %d, want %d (derived from HTML)", got.Size, len(in.HTML))
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestGetMiss(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)
	_, ok, err := c.Get(ctx, "nope")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("expected miss")
	}
}

func TestPutUpsertReplaces(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)

	c.Put(ctx, Entry{Key: "k", Pagepath: "p", HTML: []byte("v1")})
	c.Put(ctx, Entry{Key: "k", Pagepath: "p", HTML: []byte("version-two")})

	got, ok, _ := c.Get(ctx, "k")
	if !ok || string(got.HTML) != "version-two" {
		t.Errorf("expected replaced value, got ok=%v html=%q", ok, got.HTML)
	}
	if n, _ := c.Count(ctx); n != 1 {
		t.Errorf("Count = %d, want 1 (upsert, not insert)", n)
	}
	if got.Size != int64(len("version-two")) {
		t.Errorf("Size = %d, want %d", got.Size, len("version-two"))
	}
}

func TestGetTouchesLastAccess(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)

	old := time.Now().Add(-time.Hour)
	c.Put(ctx, Entry{Key: "k", Pagepath: "p", HTML: []byte("x"), LastAccess: old})

	got, ok, err := c.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !got.LastAccess.After(old) {
		t.Errorf("Get should refresh last_access: got %v, was %v", got.LastAccess, old)
	}
}

func TestKeyStableAndSensitive(t *testing.T) {
	base := Key("source", "jupyter", "env1")
	if base != Key("source", "jupyter", "env1") {
		t.Error("Key should be deterministic for identical inputs")
	}
	if base == Key("SOURCE", "jupyter", "env1") {
		t.Error("Key should change when source changes")
	}
	if base == Key("source", "knitr", "env1") {
		t.Error("Key should change when engine changes")
	}
	if base == Key("source", "jupyter", "env2") {
		t.Error("Key should change when environment fingerprint changes")
	}
	// Field boundaries must not collide due to concatenation.
	if Key("ab", "c", "") == Key("a", "bc", "") {
		t.Error("length-prefixing should prevent field-boundary collisions")
	}
}

func TestDeleteByPage(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)

	c.Put(ctx, Entry{Key: "k1", Pagepath: "report", HTML: []byte("a")})
	c.Put(ctx, Entry{Key: "k2", Pagepath: "report", HTML: []byte("b")})
	c.Put(ctx, Entry{Key: "k3", Pagepath: "other", HTML: []byte("c")})

	removed, err := c.DeleteByPage(ctx, "report")
	if err != nil {
		t.Fatalf("DeleteByPage: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if _, ok, _ := c.Get(ctx, "k3"); !ok {
		t.Error("unrelated page entry should survive")
	}
	if _, ok, _ := c.Get(ctx, "k1"); ok {
		t.Error("report entries should be gone")
	}
}

func TestTotalSize(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)
	if total, _ := c.TotalSize(ctx); total != 0 {
		t.Errorf("empty TotalSize = %d, want 0", total)
	}
	c.Put(ctx, Entry{Key: "a", Pagepath: "p", HTML: make([]byte, 100)})
	c.Put(ctx, Entry{Key: "b", Pagepath: "p", HTML: make([]byte, 50)})
	if total, _ := c.TotalSize(ctx); total != 150 {
		t.Errorf("TotalSize = %d, want 150", total)
	}
}

// putWithAccess inserts an entry and forces its last_access for deterministic
// LRU ordering.
func putWithAccess(t *testing.T, c *Cache, key string, size int, access time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := c.Put(ctx, Entry{Key: key, Pagepath: "p", HTML: make([]byte, size)}); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
	if err := c.setLastAccess(ctx, key, access); err != nil {
		t.Fatalf("setLastAccess %s: %v", key, err)
	}
}

func TestEvictBySizeLRUOrder(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)

	base := time.Now()
	// oldest -> newest by last_access.
	putWithAccess(t, c, "old", 100, base.Add(-3*time.Hour))
	putWithAccess(t, c, "mid", 100, base.Add(-2*time.Hour))
	putWithAccess(t, c, "new", 100, base.Add(-1*time.Hour))

	// Budget for two entries (250 >= 200, < 300): drop only the oldest.
	removed, err := c.EvictBySize(ctx, 250)
	if err != nil {
		t.Fatalf("EvictBySize: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, ok, _ := c.Get(ctx, "old"); ok {
		t.Error("oldest entry should have been evicted")
	}
	if _, ok, _ := c.Get(ctx, "new"); !ok {
		t.Error("newest entry should survive")
	}
	if total, _ := c.TotalSize(ctx); total != 200 {
		t.Errorf("TotalSize after evict = %d, want 200", total)
	}
}

func TestEvictBySizeNoOpWhenWithinBudget(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)
	c.Put(ctx, Entry{Key: "a", Pagepath: "p", HTML: make([]byte, 100)})
	removed, err := c.EvictBySize(ctx, 1000)
	if err != nil {
		t.Fatalf("EvictBySize: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0 (within budget)", removed)
	}
}

func TestEvictBySizeClearsWhenBudgetTooSmall(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)
	putWithAccess(t, c, "a", 100, time.Now())
	// Budget smaller than the single smallest entry: nothing fits.
	removed, err := c.EvictBySize(ctx, 10)
	if err != nil {
		t.Fatalf("EvictBySize: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if n, _ := c.Count(ctx); n != 0 {
		t.Errorf("Count = %d, want 0", n)
	}
}

func TestEvictByAge(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)

	base := time.Now()
	putWithAccess(t, c, "stale", 10, base.Add(-48*time.Hour))
	putWithAccess(t, c, "fresh", 10, base.Add(-1*time.Hour))

	removed, err := c.EvictByAge(ctx, base.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("EvictByAge: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, ok, _ := c.Get(ctx, "fresh"); !ok {
		t.Error("fresh entry should survive age eviction")
	}
	if _, ok, _ := c.Get(ctx, "stale"); ok {
		t.Error("stale entry should be evicted")
	}
}

func TestClear(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)
	c.Put(ctx, Entry{Key: "a", Pagepath: "p", HTML: []byte("x")})
	c.Put(ctx, Entry{Key: "b", Pagepath: "p", HTML: []byte("y")})
	if err := c.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if n, _ := c.Count(ctx); n != 0 {
		t.Errorf("Count after Clear = %d, want 0", n)
	}
}

func TestPutEmptyKeyRejected(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(t)
	if err := c.Put(ctx, Entry{Key: "", Pagepath: "p", HTML: []byte("x")}); err == nil {
		t.Error("expected error for empty key")
	}
}

func TestDefaultPath(t *testing.T) {
	if got := DefaultPath(":memory:"); got != ":memory:" {
		t.Errorf("DefaultPath(:memory:) = %q, want :memory:", got)
	}
	if got := DefaultPath(""); got != ":memory:" {
		t.Errorf("DefaultPath(\"\") = %q, want :memory:", got)
	}
	got := DefaultPath("/var/data/gopherwiki.sqlite")
	want := filepath.FromSlash("/var/data/render-cache.sqlite")
	if got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestPersistsToFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cache.sqlite")

	c1, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	c1.Put(ctx, Entry{Key: "k", Pagepath: "p", HTML: []byte("durable")})
	c1.Close()

	c2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer c2.Close()
	got, ok, err := c2.Get(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("expected persisted entry: ok=%v err=%v", ok, err)
	}
	if string(got.HTML) != "durable" {
		t.Errorf("HTML = %q, want durable", got.HTML)
	}
}
