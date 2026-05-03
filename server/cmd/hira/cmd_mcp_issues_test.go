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

// TestMCPIssues_InitializeThenToolsList verifies the initialize → tools/list
// handshake and that all ten expected tools are present.
func TestMCPIssues_InitializeThenToolsList(t *testing.T) {
	srv := &mcpIssuesServer{}

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
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out.String())
	}

	var toolsResp mcpResponse
	if err := json.Unmarshal([]byte(lines[1]), &toolsResp); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if toolsResp.Error != nil {
		t.Fatalf("tools/list errored: %+v", toolsResp.Error)
	}
	raw, _ := json.Marshal(toolsResp.Result)
	for _, want := range []string{
		"list_issues", "search_issues", "get_issue",
		"create_issue", "update_issue",
		"list_comments", "add_comment", "delete_comment",
		"list_task_runs", "get_task_messages",
		"inputSchema",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("tools/list missing %q: %s", want, raw)
		}
	}
}

// TestMCPIssues_ListIssuesProxiesToAPI verifies list_issues hits the right
// endpoint with query params and wraps the response in MCP text content.
func TestMCPIssues_ListIssuesProxiesToAPI(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if s := r.URL.Query().Get("status"); s != "in_progress" {
			t.Errorf("unexpected status param: %q", s)
		}
		if r.Header.Get("X-Workspace-ID") != "ws-2" {
			t.Errorf("missing workspace header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"id":"abc","title":"Fix bug"}]}`))
	}))
	defer stub.Close()

	srv := &mcpIssuesServer{api: cli.NewAPIClient(stub.URL, "ws-2", "tok")}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{"status":"in_progress","limit":5}}}` + "\n"
	var out bytes.Buffer
	if err := srv.serve(context.Background(), strings.NewReader(call), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var resp mcpResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("errored: %+v", resp.Error)
	}
	blob, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(blob), "Fix bug") {
		t.Errorf("missing upstream body: %s", blob)
	}
	if !strings.Contains(string(blob), `"type":"text"`) {
		t.Errorf("not in MCP text content shape: %s", blob)
	}
}

// TestMCPIssues_GetIssueProxiesToAPI verifies get_issue hits /api/issues/{id}.
func TestMCPIssues_GetIssueProxiesToAPI(t *testing.T) {
	const issueID = "11111111-1111-1111-1111-111111111111"
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues/"+issueID {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + issueID + `","title":"My issue"}`))
	}))
	defer stub.Close()

	srv := &mcpIssuesServer{api: cli.NewAPIClient(stub.URL, "ws-1", "tok")}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue","arguments":{"id":"` + issueID + `"}}}` + "\n"
	var out bytes.Buffer
	if err := srv.serve(context.Background(), strings.NewReader(call), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var resp mcpResponse
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp)
	if resp.Error != nil {
		t.Fatalf("errored: %+v", resp.Error)
	}
	blob, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(blob), "My issue") {
		t.Errorf("missing issue title: %s", blob)
	}
}

// TestMCPIssues_AddCommentPostsToAPI verifies add_comment issues a POST with
// content body to /api/issues/{id}/comments.
func TestMCPIssues_AddCommentPostsToAPI(t *testing.T) {
	const issueID = "22222222-2222-2222-2222-222222222222"
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/issues/"+issueID+"/comments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["content"] != "hello world" {
			t.Errorf("unexpected content: %v", body["content"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmt-1","content":"hello world"}`))
	}))
	defer stub.Close()

	srv := &mcpIssuesServer{api: cli.NewAPIClient(stub.URL, "ws-1", "tok")}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_comment","arguments":{"issue_id":"` + issueID + `","content":"hello world"}}}` + "\n"
	var out bytes.Buffer
	if err := srv.serve(context.Background(), strings.NewReader(call), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var resp mcpResponse
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp)
	if resp.Error != nil {
		t.Fatalf("errored: %+v", resp.Error)
	}
	blob, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(blob), "cmt-1") {
		t.Errorf("missing comment id in response: %s", blob)
	}
}

// TestMCPIssues_UnknownMethodReturnsMethodNotFound ensures unhandled methods
// surface as JSON-RPC -32601.
func TestMCPIssues_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	srv := &mcpIssuesServer{}
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

// TestMCPIssues_MissingRequiredParamReturnsIsError verifies that calling
// get_issue without an id surfaces as isError content (not a JSON-RPC error).
func TestMCPIssues_MissingRequiredParamReturnsIsError(t *testing.T) {
	srv := &mcpIssuesServer{}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_issue","arguments":{}}}` + "\n"
	var out bytes.Buffer
	if err := srv.serve(context.Background(), strings.NewReader(call), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp mcpResponse
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp)
	if resp.Error != nil {
		t.Fatalf("expected isError content, got JSON-RPC error: %+v", resp.Error)
	}
	blob, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(blob), `"isError":true`) {
		t.Errorf("expected isError:true, got: %s", blob)
	}
}
