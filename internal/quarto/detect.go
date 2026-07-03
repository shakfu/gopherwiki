// Package quarto integrates the Quarto CLI to render computational wiki pages.
//
// Rendering is optional and feature-detected: where the Quarto toolchain is
// absent the rest of the application runs unchanged and computational pages show
// the render-pending placeholder. Execution happens only via an authenticated,
// gated render action (never on a reader's page view), and the resulting
// self-contained HTML is stored in the render cache. This package assumes a
// trusted editing team and provides no code sandboxing. See
// docs/computational-pages.md.
package quarto

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Capabilities describes what the Quarto toolchain on this host can do. It is
// determined once at startup via Detect.
type Capabilities struct {
	// Available is true when a usable quarto binary was found.
	Available bool
	// Version is the reported quarto version (empty when unavailable).
	Version string
	// Path is the resolved absolute path to the quarto binary.
	Path string
}

// detectTimeout bounds the version probe so a wedged binary cannot hang startup.
const detectTimeout = 10 * time.Second

// Detect probes for a usable Quarto binary. path may be a bare name ("quarto")
// resolved via PATH or an absolute path; an empty path defaults to "quarto". A
// missing or non-functioning binary yields Capabilities{Available: false} rather
// than an error, so callers can treat rendering as simply unavailable.
func Detect(ctx context.Context, path string) Capabilities {
	if path == "" {
		path = "quarto"
	}

	resolved, err := exec.LookPath(path)
	if err != nil {
		return Capabilities{}
	}

	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, resolved, "--version").Output()
	if err != nil {
		// Binary exists but did not respond to --version; treat as unusable.
		return Capabilities{Path: resolved}
	}

	return Capabilities{
		Available: true,
		Version:   strings.TrimSpace(string(out)),
		Path:      resolved,
	}
}

// Fingerprint returns a string identifying the render environment for cache
// keying. A change to it invalidates cached output derived from a different
// toolchain. Today it captures the quarto version; it deliberately does not yet
// include per-engine package versions (see docs/computational-pages.md open
// decisions on environment management), so callers relying on exact package
// reproducibility should extend it.
func (c Capabilities) Fingerprint() string {
	return "quarto/" + c.Version
}
