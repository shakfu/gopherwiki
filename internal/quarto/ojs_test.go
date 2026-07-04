package quarto

import (
	"bytes"
	"strings"
	"testing"
)

// bootstrapHTML mimics Quarto's --embed-resources OJS output: a bootstrap module
// that calls interpretFromScriptTags, with NO runtime present.
const bootstrapHTML = `<html><body>
<pre class="sourceCode js">x = 1</pre>
<script type="ojs-module-contents">{"contents":[]}</script>
<script type="module">
window._ojs.paths.runtimeToDoc = "../../..";
window._ojs.runtime.interpretFromScriptTags();
</script>
</body></html>`

func TestInjectOJSRuntimeInsertsBeforeBootstrap(t *testing.T) {
	runtime := []byte("/*RUNTIME*/window._ojs={paths:{}};")
	out := injectOJSRuntime([]byte(bootstrapHTML), runtime)
	s := string(out)

	if !bytes.Contains(out, runtime) {
		t.Fatal("runtime not injected")
	}
	// The runtime must appear before the bootstrap call, so window._ojs exists
	// by the time the bootstrap runs.
	runtimeIdx := strings.Index(s, "/*RUNTIME*/")
	bootIdx := strings.Index(s, ojsBootstrapMarker)
	if runtimeIdx < 0 || bootIdx < 0 || runtimeIdx > bootIdx {
		t.Errorf("runtime (%d) must precede bootstrap (%d)", runtimeIdx, bootIdx)
	}
	// It is wrapped in its own module script.
	if !strings.Contains(s, `<script type="module">`+"\n/*RUNTIME*/") {
		t.Errorf("runtime not wrapped in a module script:\n%s", s)
	}
}

func TestInjectOJSRuntimeNoOpWithoutOJS(t *testing.T) {
	html := []byte("<html><body><p>no ojs here</p></body></html>")
	if got := injectOJSRuntime(html, []byte("RT")); !bytes.Equal(got, html) {
		t.Error("should be a no-op when there is no OJS bootstrap")
	}
}

func TestInjectOJSRuntimeNoOpWhenAlreadyPresent(t *testing.T) {
	// Page already contains the runtime's self-init marker.
	html := []byte(`<script>window._ojs={paths:{}}</script>` + bootstrapHTML)
	if got := injectOJSRuntime(html, []byte("RT")); !bytes.Equal(got, html) {
		t.Error("should not double-inject when the runtime is already present")
	}
}

func TestInjectOJSRuntimeNoOpWithEmptyRuntime(t *testing.T) {
	html := []byte(bootstrapHTML)
	if got := injectOJSRuntime(html, nil); !bytes.Equal(got, html) {
		t.Error("should be a no-op when the runtime bundle is empty")
	}
}

func TestRewriteOJSCDNs(t *testing.T) {
	in := []byte(`a="https://cdn.jsdelivr.net/npm/marked@0.3.12/marked.min.js"; ` +
		`b="https://cdn.observableusercontent.com/npm/x"; c="https://api.observablehq.com/y.js"`)
	got := string(rewriteOJSCDNs(in, "/ojs-libs"))
	want := `a="/ojs-libs/jsdelivr/npm/marked@0.3.12/marked.min.js"; ` +
		`b="/ojs-libs/observableusercontent/npm/x"; c="/ojs-libs/api-observablehq/y.js"`
	if got != want {
		t.Errorf("rewrite\n got  = %q\n want = %q", got, want)
	}
	// No external origin should remain.
	if bytes.Contains([]byte(got), []byte("https://cdn.jsdelivr.net")) {
		t.Error("jsdelivr origin still present after rewrite")
	}
}

func TestRewriteOJSCDNsNoOpWithoutBase(t *testing.T) {
	in := []byte(`x="https://cdn.jsdelivr.net/npm/d3"`)
	if got := rewriteOJSCDNs(in, ""); !bytes.Equal(got, in) {
		t.Error("empty base must be a no-op (CDN mode)")
	}
}
