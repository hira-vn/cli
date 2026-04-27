package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hira-vn/cli/server/internal/cli"
)

// TestMCPKnowledge_InitializeThenToolsList verifies the MCP stdio loop
// handles the standard initialize → tools/list handshake and returns the
// four expected tools with input schemas. No API calls are made here.
func TestMCPKnowledge_InitializeThenToolsList(t *testing.T) {
	srv := &mcpKnowledgeServer{}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n")
	var out bytes.Buffer
	if err := srv.serve(context.Background(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses (initialize + tools/list, notification is silent), got %d: %q", len(lines), out.String())
	}

	var initResp mcpResponse
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("unmarshal init: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize errored: %+v", initResp.Error)
	}

	var toolsResp mcpResponse
	if err := json.Unmarshal([]byte(lines[1]), &toolsResp); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if toolsResp.Error != nil {
		t.Fatalf("tools/list errored: %+v", toolsResp.Error)
	}
	raw, _ := json.Marshal(toolsResp.Result)
	for _, want := range []string{"search_knowledge", "list_entities", "get_entity", "get_doc", "inputSchema"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("tools/list missing %q: %s", want, raw)
		}
	}
}

// TestMCPKnowledge_SearchToolProxiesToAPI verifies that tools/call for
// search_knowledge issues a GET against /api/knowledge/search with the
// query + limit and returns the raw response body wrapped in MCP text
// content. Uses httptest for deterministic coverage.
func TestMCPKnowledge_SearchToolProxiesToAPI(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/knowledge/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if q := r.URL.Query().Get("q"); q != "refund policy" {
			t.Errorf("unexpected q: %q", q)
		}
		if lim := r.URL.Query().Get("limit"); lim != "3" {
			t.Errorf("unexpected limit: %q", lim)
		}
		if r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Errorf("missing workspace header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[{"doc_title":"Refund Policy"}]}`))
	}))
	defer stub.Close()

	srv := &mcpKnowledgeServer{
		api: cli.NewAPIClient(stub.URL, "ws-1", "token-1"),
	}

	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"refund policy","limit":3}}}` + "\n"
	var out bytes.Buffer
	if err := srv.serve(context.Background(), strings.NewReader(call), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var resp mcpResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, out.String())
	}
	if resp.Error != nil {
		t.Fatalf("tools/call errored: %+v", resp.Error)
	}
	blob, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(blob), "Refund Policy") {
		t.Errorf("missing upstream body in response: %s", blob)
	}
	if !strings.Contains(string(blob), `"type":"text"`) {
		t.Errorf("response not in MCP text content shape: %s", blob)
	}
}

// TestMCPKnowledge_UnknownMethodReturnsMethodNotFound ensures unhandled
// method strings surface as JSON-RPC -32601 rather than crashing.
func TestMCPKnowledge_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	srv := &mcpKnowledgeServer{}

	in := `{"jsonrpc":"2.0","id":9,"method":"resources/read","params":{}}` + "\n"
	var out bytes.Buffer
	if err := srv.serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var resp mcpResponse
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp)
	if resp.Error == nil || resp.Error.Code != errCodeMethodNotFound {
		t.Errorf("expected method not found, got %+v", resp.Error)
	}
}
