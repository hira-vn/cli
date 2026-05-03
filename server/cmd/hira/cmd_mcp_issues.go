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

var mcpIssuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "MCP server exposing workspace issue management tools",
	Long: `Starts a stdio MCP server with tools for managing issues, comments, and task runs:
  - list_issues(status?, priority?, assignee?, project_id?, limit?, offset?)
  - search_issues(query, limit?)
  - get_issue(id)
  - create_issue(title, description?, priority?, assignee?, parent_id?, project_id?)
  - update_issue(id, status?, priority?, title?, description?, assignee?)
  - list_comments(issue_id, limit?, offset?)
  - add_comment(issue_id, content, parent_id?)
  - delete_comment(comment_id)
  - list_task_runs(issue_id)
  - get_task_messages(task_id, since?)

Configure through the usual hira flags/env (--server-url, --workspace-id,
HIRA_TOKEN). The daemon auto-injects this server when claiming tasks.`,
	RunE: runMCPIssues,
}

func init() {
	mcpCmd.AddCommand(mcpIssuesCmd)
}

func runMCPIssues(cmd *cobra.Command, args []string) error {
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
	srv := &mcpIssuesServer{api: api}
	return srv.serve(cmd.Context(), os.Stdin, os.Stdout)
}

// --- Server ---

type mcpIssuesServer struct {
	api *cli.APIClient
}

func (s *mcpIssuesServer) serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	enc := json.NewEncoder(out)

	for {
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

func (s *mcpIssuesServer) handleLine(ctx context.Context, line []byte) (mcpResponse, bool) {
	var req mcpRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return errorResponse(nil, errCodeParse, "parse error: "+err.Error()), true
	}
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		return successResponse(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "hira-issues",
				"version": "0.1.0",
			},
		}), !isNotification

	case "notifications/initialized", "initialized":
		return mcpResponse{}, false

	case "ping":
		return successResponse(req.ID, map[string]any{}), !isNotification

	case "tools/list":
		return successResponse(req.ID, map[string]any{"tools": issueToolSchemas()}), !isNotification

	case "tools/call":
		res, err := s.dispatchTool(ctx, req.Params)
		if err != nil {
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

// --- Tool schemas ---

func issueToolSchemas() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_issues",
			"description": "List issues in the workspace with optional filters. Returns paginated results.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status":     map[string]any{"type": "string", "description": "Filter by status: todo, in_progress, in_review, done, blocked."},
					"priority":   map[string]any{"type": "string", "description": "Filter by priority: urgent, high, medium, low, none."},
					"assignee":   map[string]any{"type": "string", "description": "Filter by assignee user ID or name."},
					"project_id": map[string]any{"type": "string", "description": "Filter by project UUID."},
					"limit":      map[string]any{"type": "integer", "description": "Max results (default 20, max 50).", "minimum": 1, "maximum": 50},
					"offset":     map[string]any{"type": "integer", "description": "Pagination offset (default 0).", "minimum": 0},
				},
			},
		},
		{
			"name":        "search_issues",
			"description": "Full-text search across issue titles and descriptions.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query string."},
					"limit": map[string]any{"type": "integer", "description": "Max results (default 10, max 50).", "minimum": 1, "maximum": 50},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_issue",
			"description": "Fetch a single issue by UUID, including description, status, priority, assignee, and child issue count.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Issue UUID."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "create_issue",
			"description": "Create a new issue in the workspace.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string", "description": "Issue title."},
					"description": map[string]any{"type": "string", "description": "Issue description (markdown supported)."},
					"priority":    map[string]any{"type": "string", "description": "Priority: urgent, high, medium, low, none."},
					"assignee":    map[string]any{"type": "string", "description": "Assignee user ID."},
					"parent_id":   map[string]any{"type": "string", "description": "Parent issue UUID (creates a child issue)."},
					"project_id":  map[string]any{"type": "string", "description": "Project UUID to associate with."},
				},
				"required": []string{"title"},
			},
		},
		{
			"name":        "update_issue",
			"description": "Update an existing issue's fields. Only provided fields are changed.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "string", "description": "Issue UUID."},
					"title":       map[string]any{"type": "string", "description": "New title."},
					"description": map[string]any{"type": "string", "description": "New description (markdown)."},
					"status":      map[string]any{"type": "string", "description": "New status: todo, in_progress, in_review, done, blocked."},
					"priority":    map[string]any{"type": "string", "description": "New priority: urgent, high, medium, low, none."},
					"assignee":    map[string]any{"type": "string", "description": "Assignee user ID (empty string to unassign)."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "list_comments",
			"description": "List comments on an issue, newest first.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{"type": "string", "description": "Issue UUID."},
					"limit":    map[string]any{"type": "integer", "description": "Max results (default 20, max 100).", "minimum": 1, "maximum": 100},
					"offset":   map[string]any{"type": "integer", "description": "Pagination offset.", "minimum": 0},
				},
				"required": []string{"issue_id"},
			},
		},
		{
			"name":        "add_comment",
			"description": "Post a comment on an issue. Supports threaded replies via parent_id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id":  map[string]any{"type": "string", "description": "Issue UUID."},
					"content":   map[string]any{"type": "string", "description": "Comment content (markdown supported)."},
					"parent_id": map[string]any{"type": "string", "description": "Parent comment UUID for threading."},
				},
				"required": []string{"issue_id", "content"},
			},
		},
		{
			"name":        "delete_comment",
			"description": "Delete a comment by its UUID.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"comment_id": map[string]any{"type": "string", "description": "Comment UUID."},
				},
				"required": []string{"comment_id"},
			},
		},
		{
			"name":        "list_task_runs",
			"description": "List all agent task execution runs for an issue, including status and timestamps.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{"type": "string", "description": "Issue UUID."},
				},
				"required": []string{"issue_id"},
			},
		},
		{
			"name":        "get_task_messages",
			"description": "Get the output messages for a specific agent task run. Use `since` for incremental fetch.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Task UUID."},
					"since":   map[string]any{"type": "integer", "description": "Sequence number to fetch messages after (for incremental updates).", "minimum": 0},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

// --- Tool dispatch ---

func (s *mcpIssuesServer) dispatchTool(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var p toolCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	switch p.Name {
	case "list_issues":
		return s.toolListIssues(ctx, p.Arguments)
	case "search_issues":
		return s.toolSearchIssues(ctx, p.Arguments)
	case "get_issue":
		return s.toolGetIssue(ctx, p.Arguments)
	case "create_issue":
		return s.toolCreateIssue(ctx, p.Arguments)
	case "update_issue":
		return s.toolUpdateIssue(ctx, p.Arguments)
	case "list_comments":
		return s.toolListComments(ctx, p.Arguments)
	case "add_comment":
		return s.toolAddComment(ctx, p.Arguments)
	case "delete_comment":
		return s.toolDeleteComment(ctx, p.Arguments)
	case "list_task_runs":
		return s.toolListTaskRuns(ctx, p.Arguments)
	case "get_task_messages":
		return s.toolGetTaskMessages(ctx, p.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}
}

// --- Tool handlers ---

func (s *mcpIssuesServer) toolListIssues(ctx context.Context, args map[string]any) (map[string]any, error) {
	var parts []string
	if v, ok := args["status"].(string); ok && strings.TrimSpace(v) != "" {
		parts = append(parts, "status="+urlEscape(v))
	}
	if v, ok := args["priority"].(string); ok && strings.TrimSpace(v) != "" {
		parts = append(parts, "priority="+urlEscape(v))
	}
	if v, ok := args["assignee"].(string); ok && strings.TrimSpace(v) != "" {
		parts = append(parts, "assignee="+urlEscape(v))
	}
	if v, ok := args["project_id"].(string); ok && strings.TrimSpace(v) != "" {
		parts = append(parts, "project_id="+urlEscape(v))
	}
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 50 {
			limit = 50
		}
	}
	parts = append(parts, fmt.Sprintf("limit=%d", limit))
	if v, ok := args["offset"].(float64); ok && v > 0 {
		parts = append(parts, fmt.Sprintf("offset=%d", int(v)))
	}

	path := "/api/issues?" + strings.Join(parts, "&")
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("list issues api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolSearchIssues(ctx context.Context, args map[string]any) (map[string]any, error) {
	q, _ := args["query"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query must be a non-empty string")
	}
	limit := 10
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 50 {
			limit = 50
		}
	}
	path := fmt.Sprintf("/api/issues/search?q=%s&limit=%d", urlEscape(q), limit)
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("search issues api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolGetIssue(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/issues/"+id, &raw); err != nil {
		return nil, fmt.Errorf("get issue api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolCreateIssue(ctx context.Context, args map[string]any) (map[string]any, error) {
	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title must be a non-empty string")
	}
	body := map[string]any{"title": title}
	if v, ok := args["description"].(string); ok && v != "" {
		body["description"] = v
	}
	if v, ok := args["priority"].(string); ok && v != "" {
		body["priority"] = v
	}
	if v, ok := args["assignee"].(string); ok && v != "" {
		body["assignee"] = v
	}
	if v, ok := args["parent_id"].(string); ok && v != "" {
		body["parent_id"] = v
	}
	if v, ok := args["project_id"].(string); ok && v != "" {
		body["project_id"] = v
	}
	var raw json.RawMessage
	if err := s.api.PostJSON(ctx, "/api/issues", body, &raw); err != nil {
		return nil, fmt.Errorf("create issue api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolUpdateIssue(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	body := map[string]any{}
	if v, ok := args["title"].(string); ok {
		body["title"] = v
	}
	if v, ok := args["description"].(string); ok {
		body["description"] = v
	}
	if v, ok := args["status"].(string); ok {
		body["status"] = v
	}
	if v, ok := args["priority"].(string); ok {
		body["priority"] = v
	}
	if v, ok := args["assignee"].(string); ok {
		body["assignee"] = v
	}
	var raw json.RawMessage
	if err := s.api.PutJSON(ctx, "/api/issues/"+id, body, &raw); err != nil {
		return nil, fmt.Errorf("update issue api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolListComments(ctx context.Context, args map[string]any) (map[string]any, error) {
	issueID, _ := args["issue_id"].(string)
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return nil, fmt.Errorf("issue_id must be a non-empty string")
	}
	var parts []string
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 100 {
			limit = 100
		}
	}
	parts = append(parts, fmt.Sprintf("limit=%d", limit))
	if v, ok := args["offset"].(float64); ok && v > 0 {
		parts = append(parts, fmt.Sprintf("offset=%d", int(v)))
	}
	path := "/api/issues/" + issueID + "/comments?" + strings.Join(parts, "&")
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("list comments api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolAddComment(ctx context.Context, args map[string]any) (map[string]any, error) {
	issueID, _ := args["issue_id"].(string)
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return nil, fmt.Errorf("issue_id must be a non-empty string")
	}
	content, _ := args["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content must be a non-empty string")
	}
	body := map[string]any{"content": content}
	if v, ok := args["parent_id"].(string); ok && v != "" {
		body["parent_id"] = v
	}
	var raw json.RawMessage
	if err := s.api.PostJSON(ctx, "/api/issues/"+issueID+"/comments", body, &raw); err != nil {
		return nil, fmt.Errorf("add comment api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolDeleteComment(ctx context.Context, args map[string]any) (map[string]any, error) {
	commentID, _ := args["comment_id"].(string)
	commentID = strings.TrimSpace(commentID)
	if commentID == "" {
		return nil, fmt.Errorf("comment_id must be a non-empty string")
	}
	if err := s.api.DeleteJSON(ctx, "/api/comments/"+commentID); err != nil {
		return nil, fmt.Errorf("delete comment api: %w", err)
	}
	return textContent(`{"deleted":true}`), nil
}

func (s *mcpIssuesServer) toolListTaskRuns(ctx context.Context, args map[string]any) (map[string]any, error) {
	issueID, _ := args["issue_id"].(string)
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return nil, fmt.Errorf("issue_id must be a non-empty string")
	}
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/issues/"+issueID+"/task-runs", &raw); err != nil {
		return nil, fmt.Errorf("list task runs api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpIssuesServer) toolGetTaskMessages(ctx context.Context, args map[string]any) (map[string]any, error) {
	taskID, _ := args["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task_id must be a non-empty string")
	}
	path := "/api/tasks/" + taskID + "/messages"
	if v, ok := args["since"].(float64); ok && v > 0 {
		path += fmt.Sprintf("?since=%d", int(v))
	}
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, path, &raw); err != nil {
		return nil, fmt.Errorf("get task messages api: %w", err)
	}
	return textContent(string(raw)), nil
}
