package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hira-vn/cli/server/internal/cli"
	"github.com/spf13/cobra"
)

// mcpCmd is the parent command for Model Context Protocol stdio servers.
// Each subcommand starts a long-lived process that speaks newline-delimited
// JSON-RPC 2.0 on stdin/stdout and exposes a subset of Hira functionality
// as MCP tools. Daemons spawn these alongside agents so the agent can drill
// down into the knowledge graph (and other subsystems, eventually) mid-task.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol (MCP) stdio servers",
	Long: `Start an MCP stdio server that speaks JSON-RPC 2.0 on stdin/stdout.
These servers let AI agents call Hira tools (search knowledge, fetch docs,
etc.) through the standard MCP transport.`,
}

var mcpKnowledgeCmd = &cobra.Command{
	Use:   "knowledge",
	Short: "MCP server exposing workspace knowledge-graph tools",
	Long: `Starts a stdio MCP server with four tools:
  - search_knowledge(query, limit?)  — hybrid search across chunks
  - get_entity(id)                   — entity detail + neighbors
  - list_entities(kind?)             — browse entities (optional kind filter)
  - get_doc(id_or_slug)              — fetch a full doc by id or slug

Configure through the usual hira flags/env (--server-url, --workspace-id,
HIRA_TOKEN). The daemon spawns this server with the right credentials when
claiming a task in a workspace that has a knowledge graph.`,
	RunE: runMCPKnowledge,
}

func init() {
	mcpCmd.GroupID = groupAdditional
	mcpCmd.AddCommand(mcpKnowledgeCmd)
	rootCmd.AddCommand(mcpCmd)
}

// runMCPKnowledge is the entrypoint for `hira mcp knowledge`. It builds
// an API client from the resolved flags/env, then runs the stdio event
// loop until stdin closes or an unrecoverable I/O error occurs.
func runMCPKnowledge(cmd *cobra.Command, args []string) error {
	serverURL := resolveServerURL(cmd)
	workspaceID := resolveWorkspaceID(cmd)
	token := resolveToken(cmd)
	if serverURL == "" {
		return fmt.Errorf("server URL not configured (set --server-url or HIRA_SERVER_URL)")
	}
	if workspaceID == "" {
		return fmt.Errorf("workspace ID not configured (set --workspace-id or HIRA_WORKSPACE_ID)")
	}
	if token == "" {
		return fmt.Errorf("auth token not configured (set HIRA_TOKEN)")
	}

	api := cli.NewAPIClient(serverURL, workspaceID, token)
	srv := &mcpKnowledgeServer{api: api}
	return srv.serve(cmd.Context(), os.Stdin, os.Stdout)
}

// --- MCP JSON-RPC envelope ---

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	errCodeParse          = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

// --- Server ---

type mcpKnowledgeServer struct {
	api *cli.APIClient
}

// serve runs the read/dispatch/write loop. Notifications (requests with no
// ID) get an empty response suppressed, matching the JSON-RPC spec.
func (s *mcpKnowledgeServer) serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	enc := json.NewEncoder(out)

	for {
		// Read one line (newline-delimited JSON-RPC).
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if resp, shouldReply := s.handleLine(ctx, line); shouldReply {
				if werr := enc.Encode(resp); werr != nil {
					return fmt.Errorf("write response: %w", werr)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		}
	}
}

// handleLine parses and dispatches a single JSON-RPC message. shouldReply is
// false for notifications (no ID), so the server stays silent.
func (s *mcpKnowledgeServer) handleLine(ctx context.Context, line []byte) (mcpResponse, bool) {
	var req mcpRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return errorResponse(nil, errCodeParse, "parse error: "+err.Error()), true
	}
	// Notifications (no id) must not produce a response.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		return successResponse(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "hira-knowledge",
				"version": "0.1.0",
			},
		}), !isNotification

	case "notifications/initialized", "initialized":
		return mcpResponse{}, false

	case "ping":
		return successResponse(req.ID, map[string]any{}), !isNotification

	case "tools/list":
		return successResponse(req.ID, map[string]any{"tools": knowledgeToolSchemas()}), !isNotification

	case "tools/call":
		res, err := s.dispatchTool(ctx, req.Params)
		if err != nil {
			// MCP convention: surface tool errors as a non-isError content payload
			// so the agent can read them, rather than a JSON-RPC error.
			return successResponse(req.ID, map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
			}), !isNotification
		}
		return successResponse(req.ID, res), !isNotification

	default:
		if isNotification {
			return mcpResponse{}, false
		}
		return errorResponse(req.ID, errCodeMethodNotFound, "method not found: "+req.Method), true
	}
}

func successResponse(id json.RawMessage, result any) mcpResponse {
	return mcpResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, msg string) mcpResponse {
	return mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: code, Message: msg}}
}

// --- Tool schemas ---

// knowledgeToolSchemas lists the tools this server exposes. Keep schemas
// small and readable — the entire list gets shoved into the agent's context
// every session, so verbosity has a real cost.
func knowledgeToolSchemas() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_knowledge",
			"description": "Hybrid search (BM25 + vector + graph expansion) across the workspace knowledge graph. Returns up to `limit` ranked chunks with doc title, heading, content, and related entities.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Free-text query."},
					"limit": map[string]any{"type": "integer", "description": "Max results (default 6, max 20).", "minimum": 1, "maximum": 20},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "list_entities",
			"description": "List entities (teams, people, processes, policies, products, etc.) in the workspace graph. Optional `kind` filter.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"description": "Entity kind filter. One of: team, person, process, policy, brand_asset, product, system, metric, customer_segment.",
					},
				},
			},
		},
		{
			"name":        "get_entity",
			"description": "Fetch one entity's details plus its 1-hop neighbors in the graph.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Entity UUID."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "get_doc",
			"description": "Fetch a full published doc by UUID or slug. Returns title, category, tags, and full markdown body.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id_or_slug": map[string]any{"type": "string", "description": "Doc UUID or slug (e.g. 'refund-policy')."},
				},
				"required": []string{"id_or_slug"},
			},
		},
		{
			"name":        "create_doc",
			"description": "Create a new knowledge document in the workspace. Returns the created doc with its UUID.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":    map[string]any{"type": "string", "description": "Document title."},
					"body":     map[string]any{"type": "string", "description": "Document body in markdown."},
					"slug":     map[string]any{"type": "string", "description": "URL-friendly slug (e.g. 'refund-policy'). Auto-generated from title if omitted."},
					"category": map[string]any{"type": "string", "description": "Document category."},
					"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "List of tags."},
					"status":   map[string]any{"type": "string", "description": "draft or published (default: draft)."},
				},
				"required": []string{"title", "body"},
			},
		},
		{
			"name":        "update_doc",
			"description": "Update an existing knowledge document. Only provided fields are changed.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":       map[string]any{"type": "string", "description": "Document UUID."},
					"title":    map[string]any{"type": "string", "description": "New title."},
					"body":     map[string]any{"type": "string", "description": "New body (markdown)."},
					"slug":     map[string]any{"type": "string", "description": "New slug."},
					"category": map[string]any{"type": "string", "description": "New category."},
					"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New tags (replaces existing)."},
					"status":   map[string]any{"type": "string", "description": "New status: draft or published."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "delete_entity",
			"description": "Permanently delete a knowledge entity by UUID. Cascades to its relation edges and chunk mentions. Use list_orphan_entities to identify candidates first.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Entity UUID."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "delete_doc",
			"description": "Permanently delete a knowledge document and all its indexed chunks. Use when a doc is obsolete or replaced.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Document UUID."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "list_orphan_entities",
			"description": "List extracted entities that have no relation edges (neither as source nor target). These are candidates for cleanup — typically remnants from outdated or replaced docs.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// --- Tool dispatch ---

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// dispatchTool routes a tools/call invocation to the right handler.
// Returns the MCP content payload, or an error whose message the server will
// wrap in an isError=true response so the agent can read it.
func (s *mcpKnowledgeServer) dispatchTool(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var p toolCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	switch p.Name {
	case "search_knowledge":
		return s.toolSearchKnowledge(ctx, p.Arguments)
	case "list_entities":
		return s.toolListEntities(ctx, p.Arguments)
	case "get_entity":
		return s.toolGetEntity(ctx, p.Arguments)
	case "get_doc":
		return s.toolGetDoc(ctx, p.Arguments)
	case "create_doc":
		return s.toolCreateDoc(ctx, p.Arguments)
	case "update_doc":
		return s.toolUpdateDoc(ctx, p.Arguments)
	case "delete_entity":
		return s.toolDeleteEntity(ctx, p.Arguments)
	case "delete_doc":
		return s.toolDeleteDoc(ctx, p.Arguments)
	case "list_orphan_entities":
		return s.toolListOrphanEntities(ctx, p.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}
}

// textContent wraps a plain-text payload in the MCP content envelope so the
// agent's MCP client can hand it to the model as a string message.
func textContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

// --- Tool handlers ---

func (s *mcpKnowledgeServer) toolSearchKnowledge(ctx context.Context, args map[string]any) (map[string]any, error) {
	q, _ := args["query"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query must be a non-empty string")
	}
	limit := 6
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 20 {
			limit = 20
		}
	}

	path := fmt.Sprintf("/api/knowledge/search?q=%s&limit=%d", urlEscape(q), limit)
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("search api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpKnowledgeServer) toolListEntities(ctx context.Context, args map[string]any) (map[string]any, error) {
	path := "/api/knowledge/entities"
	if k, ok := args["kind"].(string); ok && strings.TrimSpace(k) != "" {
		path += "?kind=" + urlEscape(k)
	}
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("list entities api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpKnowledgeServer) toolGetEntity(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	// Fetch the entity and its 1-hop neighbor graph in parallel-esque sequence;
	// two small calls are still well under the daemon's spawn budget.
	var entity json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/knowledge/entities/"+id, &entity); err != nil {
		return nil, fmt.Errorf("get entity api: %w", err)
	}
	var graph json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/knowledge/entities/"+id+"/graph", &graph); err != nil {
		// Non-fatal — the entity is still useful without neighbors.
		graph = json.RawMessage("null")
	}

	combined := map[string]any{
		"entity":    json.RawMessage(entity),
		"neighbors": json.RawMessage(graph),
	}
	body, _ := json.Marshal(combined)
	return textContent(string(body)), nil
}

func (s *mcpKnowledgeServer) toolGetDoc(ctx context.Context, args map[string]any) (map[string]any, error) {
	idOrSlug, _ := args["id_or_slug"].(string)
	idOrSlug = strings.TrimSpace(idOrSlug)
	if idOrSlug == "" {
		return nil, fmt.Errorf("id_or_slug must be a non-empty string")
	}

	// Try direct by ID first (UUID-shaped input wins). Fall back to listing
	// published docs and matching slug client-side; the list endpoint is
	// already paginated enough to make that cheap.
	if looksLikeUUID(idOrSlug) {
		var raw json.RawMessage
		if err := s.api.GetJSON(ctx, "/api/knowledge/docs/"+idOrSlug, &raw); err == nil {
			return textContent(string(raw)), nil
		}
	}

	// Slug fallback. The list endpoint wraps items in {"docs":[...]}, not a
	// bare array — unwrap before scanning.
	var listed struct {
		Docs []map[string]any `json:"docs"`
	}
	if err := s.api.GetJSON(ctx, "/api/knowledge/docs?status=published", &listed); err != nil {
		return nil, fmt.Errorf("list docs api: %w", err)
	}
	for _, d := range listed.Docs {
		if slug, _ := d["slug"].(string); strings.EqualFold(slug, idOrSlug) {
			id, _ := d["id"].(string)
			if id == "" {
				break
			}
			var full json.RawMessage
			if err := s.api.GetJSON(ctx, "/api/knowledge/docs/"+id, &full); err != nil {
				return nil, fmt.Errorf("get doc api: %w", err)
			}
			return textContent(string(full)), nil
		}
	}
	return nil, fmt.Errorf("no doc found for id_or_slug %q", idOrSlug)
}

func (s *mcpKnowledgeServer) toolCreateDoc(ctx context.Context, args map[string]any) (map[string]any, error) {
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title must be a non-empty string")
	}
	body, _ := args["body"].(string)
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("body must be a non-empty string")
	}
	payload := map[string]any{"title": title, "body": body}
	if v, ok := args["slug"].(string); ok && v != "" {
		payload["slug"] = v
	}
	if v, ok := args["category"].(string); ok && v != "" {
		payload["category"] = v
	}
	if v, ok := args["tags"]; ok {
		payload["tags"] = v
	}
	if v, ok := args["status"].(string); ok && v != "" {
		payload["status"] = v
	}
	var raw json.RawMessage
	if err := s.api.PostJSON(ctx, "/api/knowledge/docs", payload, &raw); err != nil {
		return nil, fmt.Errorf("create doc api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpKnowledgeServer) toolUpdateDoc(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	payload := map[string]any{}
	if v, ok := args["title"].(string); ok {
		payload["title"] = v
	}
	if v, ok := args["body"].(string); ok {
		payload["body"] = v
	}
	if v, ok := args["slug"].(string); ok {
		payload["slug"] = v
	}
	if v, ok := args["category"].(string); ok {
		payload["category"] = v
	}
	if v, ok := args["tags"]; ok {
		payload["tags"] = v
	}
	if v, ok := args["status"].(string); ok {
		payload["status"] = v
	}
	var raw json.RawMessage
	if err := s.api.PutJSON(ctx, "/api/knowledge/docs/"+id, payload, &raw); err != nil {
		return nil, fmt.Errorf("update doc api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpKnowledgeServer) toolDeleteEntity(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	if err := s.api.DeleteJSON(ctx, "/api/knowledge/entities/"+id); err != nil {
		return nil, fmt.Errorf("delete entity api: %w", err)
	}
	return textContent(`{"deleted":true}`), nil
}

func (s *mcpKnowledgeServer) toolDeleteDoc(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	if err := s.api.DeleteJSON(ctx, "/api/knowledge/docs/"+id); err != nil {
		return nil, fmt.Errorf("delete doc api: %w", err)
	}
	return textContent(`{"deleted":true}`), nil
}

func (s *mcpKnowledgeServer) toolListOrphanEntities(ctx context.Context, _ map[string]any) (map[string]any, error) {
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/knowledge/entities?orphan=true", &raw); err != nil {
		return nil, fmt.Errorf("list orphan entities api: %w", err)
	}
	return textContent(string(raw)), nil
}

// --- helpers ---

// urlEscape is a thin wrapper so we can swap to url.QueryEscape without
// pulling the whole net/url import list into cmd_mcp.go's call sites.
func urlEscape(s string) string {
	// Conservative — replace the characters that matter for our query shapes
	// (spaces + &) via QueryEscape.
	return queryEscape(s)
}

func queryEscape(s string) string {
	// Implemented inline to avoid an extra import section for a single call.
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteByte('+')
		case r == '&' || r == '=' || r == '?' || r == '#' || r == '%':
			fmt.Fprintf(&b, "%%%02X", r)
		case r < 0x80:
			b.WriteRune(r)
		default:
			for _, bb := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", bb)
			}
		}
	}
	return b.String()
}

// looksLikeUUID returns true when s is 36 chars with dashes in canonical
// UUID positions. Good enough to decide whether to try /docs/{id} first.
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	return s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
