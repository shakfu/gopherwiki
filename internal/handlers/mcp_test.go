package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

// mcpPost sends one JSON-RPC message to the MCP endpoint.
func mcpPost(t *testing.T, env *testutil.TestEnv, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/-/api/v1/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	return w
}

// mcpReply decodes a JSON-RPC reply.
func mcpReply(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status = %d, content type %q; body: %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	var reply map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return reply
}

// mcpToolCall calls a tool and returns its text and isError flag.
func mcpToolCall(t *testing.T, env *testutil.TestEnv, token, tool string, args interface{}) (string, bool) {
	t.Helper()
	a, _ := json.Marshal(args)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, a)
	reply := mcpReply(t, mcpPost(t, env, token, body, nil))
	result, ok := reply["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("%s: no result: %v", tool, reply)
	}
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	return text, result["isError"] == true
}

func TestMCP_Lifecycle(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)

	reply := mcpReply(t, mcpPost(t, env, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`, nil))
	result := reply["result"].(map[string]interface{})
	if result["protocolVersion"] != "2025-06-18" || result["capabilities"].(map[string]interface{})["tools"] == nil {
		t.Errorf("initialize result = %v", result)
	}
	if reply["id"].(float64) != 1 {
		t.Errorf("reply id = %v, want 1", reply["id"])
	}

	reply = mcpReply(t, mcpPost(t, env, token, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`, nil))
	if v := reply["result"].(map[string]interface{})["protocolVersion"]; v != "2025-11-25" {
		t.Errorf("unsupported client version: server offered %v, want 2025-11-25", v)
	}

	if w := mcpPost(t, env, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Errorf("notification: status = %d, body %q; want 202, empty", w.Code, w.Body.String())
	}

	reply = mcpReply(t, mcpPost(t, env, token, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, nil))
	var names []string
	for _, tool := range reply["result"].(map[string]interface{})["tools"].([]interface{}) {
		names = append(names, tool.(map[string]interface{})["name"].(string))
	}
	for _, want := range []string{"guide", "search", "read_page", "write_pages", "list_runs", "read_run", "backlinks", "lint", "changelog", "open_issue"} {
		if !strings.Contains(strings.Join(names, " "), want) {
			t.Errorf("tools/list missing %s", want)
		}
	}

	reply = mcpReply(t, mcpPost(t, env, token, `{"jsonrpc":"2.0","id":4,"method":"resources/list"}`, nil))
	if reply["error"].(map[string]interface{})["code"].(float64) != -32601 {
		t.Errorf("unknown method: %v", reply)
	}
	reply = mcpReply(t, mcpPost(t, env, token, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}`, nil))
	if reply["error"].(map[string]interface{})["code"].(float64) != -32602 {
		t.Errorf("unknown tool: %v", reply)
	}
}

func TestMCP_TransportRejections(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	if w := mcpPost(t, env, token, ping, map[string]string{"Origin": "https://evil.example"}); w.Code != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403", w.Code)
	}
	if w := mcpPost(t, env, token, ping, map[string]string{"Origin": "http://localhost:8080"}); w.Code != http.StatusOK {
		t.Errorf("site origin: status = %d, want 200", w.Code)
	}
	if w := mcpPost(t, env, token, ping, map[string]string{"MCP-Protocol-Version": "2000-01-01"}); w.Code != http.StatusBadRequest {
		t.Errorf("unsupported protocol header: status = %d, want 400", w.Code)
	}
	if w := mcpPost(t, env, token, `[`+ping+`]`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("batch body: status = %d, want 400", w.Code)
	}
	if w := mcpPost(t, env, "gw_wrong", ping, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("bad token: status = %d, want 401", w.Code)
	}

	req := httptest.NewRequest("GET", "/-/api/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, want 405", w.Code)
	}
}

func TestMCP_Tools(t *testing.T) {
	env, _ := runEnv(t)
	token := createAPIToken(t, env, false)
	run := runPeriod(t, env, "2026-08")
	env.Server.Wiki.SavePage(t.Context(), "Meta/Schema", "Analyses live under kb/analyses.\n", "", "", author)

	text, isErr := mcpToolCall(t, env, token, "guide", map[string]interface{}{})
	for _, want := range []string{`at or below "kb"`, "Analyses live under kb/analyses.", "- analysis"} {
		if isErr || !strings.Contains(text, want) {
			t.Errorf("guide missing %q:\n%s", want, text)
		}
	}

	text, isErr = mcpToolCall(t, env, token, "read_run", map[string]interface{}{"path": "analysis", "id": run})
	if isErr || !strings.Contains(text, "| 2026-08 | 42 |") {
		t.Errorf("read_run = %q, isError %v", text, isErr)
	}

	page := fmt.Sprintf("---\nsources:\n  - page: analysis\n    run: %d\n---\nRevenue was 42.\n", run)
	text, isErr = mcpToolCall(t, env, token, "write_pages", map[string]interface{}{
		"message": "August analysis",
		"pages":   []map[string]string{{"path": "kb/august", "content": page}},
	})
	if isErr || !strings.Contains(text, `"changed": true`) {
		t.Fatalf("write_pages = %q, isError %v", text, isErr)
	}

	text, isErr = mcpToolCall(t, env, token, "read_page", map[string]interface{}{"path": "kb/august"})
	if isErr || !strings.Contains(text, "Revenue was 42.") || !strings.Contains(text, `"period": "2026-08"`) {
		t.Errorf("read_page = %q, isError %v", text, isErr)
	}

	// The API's rules reach the model as tool errors.
	text, isErr = mcpToolCall(t, env, token, "write_pages", map[string]interface{}{
		"pages": []map[string]string{{"path": "home", "content": "x"}},
	})
	if !isErr || !strings.Contains(text, "HTTP 403") {
		t.Errorf("write outside prefix = %q, isError %v; want a 403 tool error", text, isErr)
	}
	text, isErr = mcpToolCall(t, env, token, "read_run", map[string]interface{}{"path": "analysis"})
	if !isErr || !strings.Contains(text, "id") {
		t.Errorf("read_run without id = %q, isError %v", text, isErr)
	}

	if text, isErr = mcpToolCall(t, env, token, "open_issue", map[string]interface{}{"title": "Fix Home"}); isErr {
		t.Errorf("open_issue = %q", text)
	}
	if text, isErr = mcpToolCall(t, env, token, "lint", map[string]interface{}{}); isErr || !strings.Contains(text, "kb/august") {
		t.Errorf("lint = %q, isError %v", text, isErr)
	}
	if text, isErr = mcpToolCall(t, env, token, "search", map[string]interface{}{"query": "Revenue"}); isErr || !strings.Contains(text, "kb/august") {
		t.Errorf("search = %q, isError %v", text, isErr)
	}
}
