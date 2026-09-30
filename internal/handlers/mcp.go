package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/middleware"
)

// MCP (Model Context Protocol) endpoint over the Streamable HTTP transport.
// It is stateless and answers every request with one JSON object; it offers no
// SSE stream and no sessions. Each tool calls the JSON API in-process with the
// caller's credentials, so tokens, permissions and hidden pages behave exactly
// as they do for the API. See docs/API.md.

// mcpVersions are the protocol versions served, newest first. The tool
// messages used here are the same in all of them.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

// mcpMessage is a JSON-RPC 2.0 message.
type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpTool is one tool: its description, input schema, and the API request
// that implements it.
type mcpTool struct {
	Name        string
	Description string
	Schema      map[string]any
	// request returns the API method, path and JSON body for the arguments.
	request func(args map[string]any) (method, path string, body any, err error)
}

// Schema helpers.
func mcpObject(required []string, props map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func mcpString(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// argString returns a required string argument.
func argString(args map[string]any, name string) (string, error) {
	v, _ := args[name].(string)
	if v == "" {
		return "", fmt.Errorf("argument %q is required", name)
	}
	return v, nil
}

// apiPath builds an escaped API path for a page, with an optional suffix.
func apiPath(page, suffix string) string {
	return (&url.URL{Path: "/-/api/v1/pages/" + strings.Trim(page, "/") + suffix}).String()
}

var mcpTools = []mcpTool{
	{
		Name:        "guide",
		Description: "Read first. Explains how to write in this wiki, where you may write, and lists the pages.",
		Schema:      mcpObject(nil, map[string]any{}),
	},
	{
		Name:        "search",
		Description: "Full-text search over pages.",
		Schema:      mcpObject([]string{"query"}, map[string]any{"query": mcpString("Search terms")}),
		request: func(args map[string]any) (string, string, any, error) {
			q, err := argString(args, "query")
			return "GET", "/-/api/v1/search?q=" + url.QueryEscape(q), nil, err
		},
	},
	{
		Name:        "read_page",
		Description: "Read a page: its source, metadata.revision (needed to overwrite it), and the state of the report runs it cites.",
		Schema: mcpObject([]string{"path"}, map[string]any{
			"path":     mcpString("Page path, such as kb/august"),
			"revision": mcpString("Optional older revision to read"),
		}),
		request: func(args map[string]any) (string, string, any, error) {
			p, err := argString(args, "path")
			path := apiPath(p, "")
			if rev, _ := args["revision"].(string); rev != "" {
				path += "?revision=" + url.QueryEscape(rev)
			}
			return "GET", path, nil, err
		},
	},
	{
		Name:        "write_pages",
		Description: "Create or update one or more pages in one commit. To overwrite a page, pass the metadata.revision from read_page. A path ending in .qmd creates a computational page.",
		Schema: mcpObject([]string{"pages"}, map[string]any{
			"message": mcpString("Commit message"),
			"pages": map[string]any{
				"type": "array",
				"items": mcpObject([]string{"path", "content"}, map[string]any{
					"path":     mcpString("Page path"),
					"content":  mcpString("Full page source, including frontmatter"),
					"revision": mcpString("Base revision; required when the page exists"),
				}),
			},
		}),
		request: func(args map[string]any) (string, string, any, error) {
			if _, ok := args["pages"].([]any); !ok {
				return "", "", nil, fmt.Errorf("argument %q is required", "pages")
			}
			return "POST", "/-/api/v1/batch", args, nil
		},
	},
	{
		Name:        "list_runs",
		Description: "List the report runs of a computational page, newest first.",
		Schema:      mcpObject([]string{"path"}, map[string]any{"path": mcpString("Report page path")}),
		request: func(args map[string]any) (string, string, any, error) {
			p, err := argString(args, "path")
			return "GET", apiPath(p, "/runs"), nil, err
		},
	},
	{
		Name:        "read_run",
		Description: "Read one report run: the executed markdown holding the report's figures.",
		Schema: mcpObject([]string{"path", "id"}, map[string]any{
			"path": mcpString("Report page path"),
			"id":   map[string]any{"type": "integer", "description": "Run ID from list_runs"},
		}),
		request: func(args map[string]any) (string, string, any, error) {
			p, err := argString(args, "path")
			id, ok := args["id"].(float64)
			if err == nil && (!ok || id <= 0 || id != float64(int64(id))) {
				err = fmt.Errorf("argument %q must be a positive integer", "id")
			}
			return "GET", apiPath(p, fmt.Sprintf("/runs/%d", int64(id))), nil, err
		},
	},
	{
		Name:        "backlinks",
		Description: "List the pages that link to a page.",
		Schema:      mcpObject([]string{"path"}, map[string]any{"path": mcpString("Page path")}),
		request: func(args map[string]any) (string, string, any, error) {
			p, err := argString(args, "path")
			return "GET", apiPath(p, "/backlinks"), nil, err
		},
	},
	{
		Name:        "lint",
		Description: "List deterministic findings: broken links, orphans, and for your pages missing validation, stale or missing sources, and numbers not found in the cited runs.",
		Schema:      mcpObject(nil, map[string]any{}),
		request: func(map[string]any) (string, string, any, error) {
			return "GET", "/-/api/v1/lint", nil, nil
		},
	},
	{
		Name:        "changelog",
		Description: "List recent commits.",
		Schema:      mcpObject(nil, map[string]any{}),
		request: func(map[string]any) (string, string, any, error) {
			return "GET", "/-/api/v1/changelog", nil, nil
		},
	},
	{
		Name:        "open_issue",
		Description: "Open an issue for the human editors, for example to propose a change to a page you may not write.",
		Schema: mcpObject([]string{"title"}, map[string]any{
			"title":       mcpString("Issue title"),
			"description": mcpString("Issue description"),
		}),
		request: func(args map[string]any) (string, string, any, error) {
			title, err := argString(args, "title")
			desc, _ := args["description"].(string)
			return "POST", "/-/api/v1/issues", map[string]string{"title": title, "description": desc}, err
		},
	},
}

// handleMCP handles POST /-/api/v1/mcp.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// No server-initiated stream and no sessions to delete.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if !s.mcpOriginAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !mcpSupported(v) {
		http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var msg mcpMessage
	if err := json.Unmarshal(body, &msg); err != nil || msg.JSONRPC != "2.0" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "error": map[string]any{"code": -32600, "message": "invalid request"}})
		return
	}

	// Notifications and responses need no reply.
	if len(msg.ID) == 0 || msg.Method == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := s.mcpDispatch(r, msg)
	reply := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
	if rpcErr != nil {
		reply["error"] = rpcErr
	} else {
		reply["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}

// mcpOriginAllowed rejects browser requests from other sites, which could
// otherwise reach a local server through DNS rebinding. Clients that send no
// Origin, such as agents, are allowed.
func (s *Server) mcpOriginAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	o, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if site, err := url.Parse(s.Config.SiteURL); err == nil && o.Scheme == site.Scheme && o.Host == site.Host {
		return true
	}
	return o.Host == r.Host
}

func mcpSupported(version string) bool {
	for _, v := range mcpVersions {
		if v == version {
			return true
		}
	}
	return false
}

// mcpDispatch answers one JSON-RPC request.
func (s *Server) mcpDispatch(r *http.Request, msg mcpMessage) (any, map[string]any) {
	switch msg.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(msg.Params, &params)
		version := mcpVersions[0]
		if mcpSupported(params.ProtocolVersion) {
			version = params.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "gopherwiki", "version": s.Version},
			"instructions":    "Call the guide tool before writing.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := make([]map[string]any, 0, len(mcpTools))
		for _, t := range mcpTools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, map[string]any{"code": -32602, "message": "invalid params"}
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		for _, t := range mcpTools {
			if t.Name == params.Name {
				return s.mcpCall(r, t, params.Arguments), nil
			}
		}
		return nil, map[string]any{"code": -32602, "message": "unknown tool: " + params.Name}
	default:
		return nil, map[string]any{"code": -32601, "message": "method not found: " + msg.Method}
	}
}

// mcpText is a tool result holding one text block.
func mcpText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// mcpCall runs a tool. A failed call is a tool result with isError set, so the
// model sees the reason, rather than a protocol error.
func (s *Server) mcpCall(r *http.Request, t mcpTool, args map[string]any) map[string]any {
	if t.Name == "guide" {
		return s.mcpGuide(r)
	}
	method, path, body, err := t.request(args)
	if err != nil {
		return mcpText(err.Error(), true)
	}
	status, data, errMsg := s.apiCall(r, method, path, body)
	if errMsg != "" {
		return mcpText(fmt.Sprintf("HTTP %d: %s", status, errMsg), true)
	}
	return mcpText(data, false)
}

// apiCall performs a JSON API request in-process with the caller's
// credentials. It returns the status and either the indented "data" or the
// error message.
func (s *Server) apiCall(r *http.Request, method, path string, body any) (int, string, string) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return http.StatusBadRequest, "", err.Error()
		}
		reader = bytes.NewReader(b)
	}
	// chi routes a request that already carries a route context as if it came
	// from a parent router, so the outer request's route context is removed.
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, (*chi.Context)(nil))
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return http.StatusBadRequest, "", err.Error()
	}
	for _, h := range []string{"Authorization", "Cookie", middleware.CSRFHeaderName} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")

	rec := &responseBuffer{header: http.Header{}, status: http.StatusOK}
	s.router.ServeHTTP(rec, req)

	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(rec.body.Bytes(), &envelope); err != nil {
		return rec.status, "", strings.TrimSpace(rec.body.String())
	}
	if rec.status >= 400 || envelope.Error != "" {
		return rec.status, "", envelope.Error
	}
	var out bytes.Buffer
	if err := json.Indent(&out, envelope.Data, "", "  "); err != nil {
		return rec.status, string(envelope.Data), ""
	}
	return rec.status, out.String(), ""
}

// responseBuffer collects an in-process response.
type responseBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *responseBuffer) Header() http.Header         { return b.header }
func (b *responseBuffer) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *responseBuffer) WriteHeader(status int)      { b.status = status }

// mcpGuide explains the wiki's mechanics to the agent, then adds the wiki's
// own conventions from the guide page and the list of pages.
func (s *Server) mcpGuide(r *http.Request) map[string]any {
	var b strings.Builder
	b.WriteString("# GopherWiki agent guide\n\n")
	if token := middleware.GetToken(r); token != nil {
		fmt.Fprintf(&b, "- You may write pages only at or below %q. For any other page, use open_issue.\n", token.WritePrefix)
	}
	b.WriteString(`- write_pages saves one or more pages in one commit. To overwrite a page, pass its metadata.revision from read_page; a stale revision fails with a conflict, so read the page again.
- A reviewer validates each revision you write. Any later commit voids the validation.
- Report figures come from report runs: list_runs and read_run return a report's executed markdown. Write the prose; take figures from the runs.
- Cite every run you use in the page frontmatter:

  ---
  sources:
    - page: reports/sales
      run: 12
  ---

- lint reports stale or missing sources and numbers not found in the cited runs, for the reviewer to check.
- To propose a new report, write a page whose path ends in .qmd. A reviewer must approve its code; you cannot run it.
`)

	guidePage := s.Config.GuidePage
	if guidePage != "" {
		if _, data, errMsg := s.apiCall(r, "GET", apiPath(guidePage, ""), nil); errMsg == "" {
			var page struct {
				Content string `json:"content"`
			}
			json.Unmarshal([]byte(data), &page)
			fmt.Fprintf(&b, "\n# Wiki conventions (page %s)\n\n%s\n", guidePage, page.Content)
		}
	}

	if _, data, errMsg := s.apiCall(r, "GET", "/-/api/v1/pages", nil); errMsg == "" {
		var pages []struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(data), &pages)
		b.WriteString("\n# Pages\n\n")
		for _, p := range pages {
			b.WriteString("- " + p.Path + "\n")
		}
	}
	return mcpText(b.String(), false)
}
