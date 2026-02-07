// Package web provides embedded static assets and templates for GopherWiki.
package web

import "embed"

//go:embed templates/*.html
var TemplatesFS embed.FS

//go:embed static
var StaticFS embed.FS
