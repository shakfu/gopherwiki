package quarto

import "bytes"

// Quarto renders Observable JS (OJS) cells with a small bootstrap script that
// calls window._ojs.runtime.interpretFromScriptTags(), but --embed-resources
// does NOT inline the OJS runtime that defines window._ojs. The result is a page
// whose OJS cells never execute (they show as inert source). To make OJS work in
// a self-contained blob, the runtime bundle is injected just before the bootstrap
// so window._ojs is defined first. See docs/computational-pages.md.
const (
	// ojsBootstrapMarker appears in the per-document bootstrap; its presence means
	// the page has OJS cells to interpret.
	ojsBootstrapMarker = "_ojs.runtime.interpretFromScriptTags"
	// ojsRuntimePresentMarker is the runtime bundle's own self-initialization of
	// window._ojs. If already present (e.g. a future Quarto that embeds it, or a
	// page already injected), no injection is needed.
	ojsRuntimePresentMarker = "window._ojs="
)

// injectOJSRuntime returns htmlBytes with the OJS runtime bundle inserted as an
// inline module immediately before Quarto's OJS bootstrap script, so the runtime
// executes first and defines window._ojs. It is a no-op when the page has no OJS
// cells, when the runtime is already present, or when runtime is empty. Module
// scripts execute in document order, so inserting before the bootstrap guarantees
// the runtime runs first.
func injectOJSRuntime(htmlBytes, runtime []byte) []byte {
	if len(runtime) == 0 {
		return htmlBytes
	}
	if !bytes.Contains(htmlBytes, []byte(ojsBootstrapMarker)) {
		return htmlBytes // no OJS cells
	}
	if bytes.Contains(htmlBytes, []byte(ojsRuntimePresentMarker)) {
		return htmlBytes // runtime already embedded
	}
	idx := bytes.Index(htmlBytes, []byte(ojsBootstrapMarker))
	open := bytes.LastIndex(htmlBytes[:idx], []byte("<script"))
	if open < 0 {
		return htmlBytes // unexpected structure; leave untouched
	}

	var b bytes.Buffer
	b.Grow(len(htmlBytes) + len(runtime) + 64)
	b.Write(htmlBytes[:open])
	b.WriteString(`<script type="module">` + "\n")
	b.Write(runtime)
	b.WriteString("\n</script>\n")
	b.Write(htmlBytes[open:])
	return b.Bytes()
}

// ojsCDNHosts maps the external Observable/jsDelivr origins the runtime fetches
// its standard library (and imports/attachments) from to per-host subpaths under
// a local mirror. Rewriting these lets OJS pages load their libraries from the
// wiki itself, with no external network access at view time.
var ojsCDNHosts = []struct{ origin, sub string }{
	{"https://cdn.jsdelivr.net/", "jsdelivr/"},
	{"https://cdn.observableusercontent.com/", "observableusercontent/"},
	{"https://api.observablehq.com/", "api-observablehq/"},
}

// rewriteOJSCDNs replaces the external Observable CDN origins in htmlBytes with
// local paths under base (e.g. "/ojs-libs"), so the runtime loads from the wiki's
// own mirror. base must not end with a slash. A no-op when base is empty.
func rewriteOJSCDNs(htmlBytes []byte, base string) []byte {
	if base == "" {
		return htmlBytes
	}
	out := htmlBytes
	for _, h := range ojsCDNHosts {
		out = bytes.ReplaceAll(out, []byte(h.origin), []byte(base+"/"+h.sub))
	}
	return out
}
