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

var mcpSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "MCP server exposing workspace skill management tools",
	Long: `Starts a stdio MCP server with tools for managing agent skills:
  - list_skills()
  - get_skill(id)
  - create_skill(name, description?, content?)
  - update_skill(id, name?, description?, content?)
  - upsert_skill_file(skill_id, filename, content)

Configure through the usual hira flags/env (--server-url, --workspace-id,
HIRA_TOKEN).`,
	RunE: runMCPSkills,
}

func init() {
	mcpCmd.AddCommand(mcpSkillsCmd)
}

func runMCPSkills(cmd *cobra.Command, args []string) error {
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
	srv := &mcpSkillsServer{api: api}
	return srv.serve(cmd.Context(), os.Stdin, os.Stdout)
}

// --- Server ---

type mcpSkillsServer struct {
	api *cli.APIClient
}

func (s *mcpSkillsServer) serve(ctx context.Context, in io.Reader, out io.Writer) error {
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

func (s *mcpSkillsServer) handleLine(ctx context.Context, line []byte) (mcpResponse, bool) {
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
				"name":    "hira-skills",
				"version": "0.1.0",
			},
		}), !isNotification

	case "notifications/initialized", "initialized":
		return mcpResponse{}, false

	case "ping":
		return successResponse(req.ID, map[string]any{}), !isNotification

	case "tools/list":
		return successResponse(req.ID, map[string]any{"tools": skillToolSchemas()}), !isNotification

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

func skillToolSchemas() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_skills",
			"description": "List all skills in the workspace.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "get_skill",
			"description": "Fetch a skill by UUID, including its description and file list.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Skill UUID."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "create_skill",
			"description": "Create a new skill in the workspace.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "Skill name."},
					"description": map[string]any{"type": "string", "description": "What the skill does and when to use it."},
					"content":     map[string]any{"type": "string", "description": "Skill content/instructions (markdown)."},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "update_skill",
			"description": "Update an existing skill. Only provided fields are changed.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":          map[string]any{"type": "string", "description": "Skill UUID."},
					"name":        map[string]any{"type": "string", "description": "New name."},
					"description": map[string]any{"type": "string", "description": "New description."},
					"content":     map[string]any{"type": "string", "description": "New content/instructions."},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "upsert_skill_file",
			"description": "Create or replace a file within a skill. Use this to write skill instructions, templates, or reference documents.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_id": map[string]any{"type": "string", "description": "Skill UUID."},
					"filename": map[string]any{"type": "string", "description": "File name within the skill (e.g. 'instructions.md')."},
					"content":  map[string]any{"type": "string", "description": "File content."},
				},
				"required": []string{"skill_id", "filename", "content"},
			},
		},
	}
}

// --- Tool dispatch ---

func (s *mcpSkillsServer) dispatchTool(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var p toolCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	switch p.Name {
	case "list_skills":
		return s.toolListSkills(ctx, p.Arguments)
	case "get_skill":
		return s.toolGetSkill(ctx, p.Arguments)
	case "create_skill":
		return s.toolCreateSkill(ctx, p.Arguments)
	case "update_skill":
		return s.toolUpdateSkill(ctx, p.Arguments)
	case "upsert_skill_file":
		return s.toolUpsertSkillFile(ctx, p.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}
}

// --- Tool handlers ---

func (s *mcpSkillsServer) toolListSkills(ctx context.Context, _ map[string]any) (map[string]any, error) {
	var raw json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/skills", &raw); err != nil {
		return nil, fmt.Errorf("list skills api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpSkillsServer) toolGetSkill(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	var skill json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/skills/"+id, &skill); err != nil {
		return nil, fmt.Errorf("get skill api: %w", err)
	}
	var files json.RawMessage
	if err := s.api.GetJSON(ctx, "/api/skills/"+id+"/files", &files); err != nil {
		files = json.RawMessage("null")
	}
	combined := map[string]any{
		"skill": json.RawMessage(skill),
		"files": json.RawMessage(files),
	}
	body, _ := json.Marshal(combined)
	return textContent(string(body)), nil
}

func (s *mcpSkillsServer) toolCreateSkill(ctx context.Context, args map[string]any) (map[string]any, error) {
	name, _ := args["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name must be a non-empty string")
	}
	body := map[string]any{"name": name}
	if v, ok := args["description"].(string); ok && v != "" {
		body["description"] = v
	}
	if v, ok := args["content"].(string); ok && v != "" {
		body["content"] = v
	}
	var raw json.RawMessage
	if err := s.api.PostJSON(ctx, "/api/skills", body, &raw); err != nil {
		return nil, fmt.Errorf("create skill api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpSkillsServer) toolUpdateSkill(ctx context.Context, args map[string]any) (map[string]any, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id must be a non-empty string")
	}
	body := map[string]any{}
	if v, ok := args["name"].(string); ok {
		body["name"] = v
	}
	if v, ok := args["description"].(string); ok {
		body["description"] = v
	}
	if v, ok := args["content"].(string); ok {
		body["content"] = v
	}
	var raw json.RawMessage
	if err := s.api.PutJSON(ctx, "/api/skills/"+id, body, &raw); err != nil {
		return nil, fmt.Errorf("update skill api: %w", err)
	}
	return textContent(string(raw)), nil
}

func (s *mcpSkillsServer) toolUpsertSkillFile(ctx context.Context, args map[string]any) (map[string]any, error) {
	skillID, _ := args["skill_id"].(string)
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return nil, fmt.Errorf("skill_id must be a non-empty string")
	}
	filename, _ := args["filename"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, fmt.Errorf("filename must be a non-empty string")
	}
	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content must be a non-empty string")
	}
	body := map[string]any{
		"filename": filename,
		"content":  content,
	}
	var raw json.RawMessage
	if err := s.api.PutJSON(ctx, "/api/skills/"+skillID+"/files", body, &raw); err != nil {
		return nil, fmt.Errorf("upsert skill file api: %w", err)
	}
	return textContent(string(raw)), nil
}
