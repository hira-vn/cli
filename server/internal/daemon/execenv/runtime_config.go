package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InjectRuntimeConfig writes the meta skill content into the runtime-specific
// config file so the agent discovers its environment through its native mechanism.
//
// For Claude:   writes {workDir}/CLAUDE.md  (skills discovered natively from .claude/skills/)
// For Codex:    writes {workDir}/AGENTS.md  (skills discovered natively via CODEX_HOME)
// For Copilot:  writes {workDir}/AGENTS.md  (skills discovered natively from .github/skills/)
// For OpenCode: writes {workDir}/AGENTS.md  (skills discovered natively from .config/opencode/skills/)
// For OpenClaw: writes {workDir}/AGENTS.md  (skills discovered natively from .openclaw/skills/)
// For Gemini:   writes {workDir}/GEMINI.md  (discovered natively by the Gemini CLI)
// For Pi:       writes {workDir}/AGENTS.md  (skills discovered natively from ~/.pi/agent/skills/)
// For Cursor:   writes {workDir}/AGENTS.md  (skills discovered natively from .cursor/skills/)
func InjectRuntimeConfig(workDir, provider string, ctx TaskContextForEnv) error {
	content := buildMetaSkillContent(provider, ctx)

	switch provider {
	case "claude":
		return os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	case "codex", "copilot", "opencode", "openclaw", "pi", "cursor":
		return os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(content), 0o644)
	case "gemini":
		return os.WriteFile(filepath.Join(workDir, "GEMINI.md"), []byte(content), 0o644)
	default:
		// Unknown provider — skip config injection, prompt-only mode.
		return nil
	}
}

// buildMetaSkillContent generates the meta skill markdown that teaches the agent
// about the Hira runtime environment and available CLI tools.
func buildMetaSkillContent(provider string, ctx TaskContextForEnv) string {
	var b strings.Builder

	b.WriteString("# Hira Agent Runtime\n\n")
	b.WriteString("You are a coding agent in the Hira platform. Use the `hira` CLI to interact with the platform.\n\n")

	// Always emit agent identity so the agent knows who it is, even when
	// dispatched via @mention on an issue assigned to a different agent.
	if ctx.AgentName != "" || ctx.AgentID != "" {
		b.WriteString("## Agent Identity\n\n")
		if ctx.AgentName != "" {
			fmt.Fprintf(&b, "**You are: %s**", ctx.AgentName)
			if ctx.AgentID != "" {
				fmt.Fprintf(&b, " (ID: `%s`)", ctx.AgentID)
			}
			b.WriteString("\n\n")
		}
		if ctx.AgentInstructions != "" {
			b.WriteString(ctx.AgentInstructions)
			b.WriteString("\n\n")
		}
	} else if ctx.AgentInstructions != "" {
		b.WriteString("## Agent Identity\n\n")
		b.WriteString(ctx.AgentInstructions)
		b.WriteString("\n\n")
	}

	b.WriteString("## Available Commands\n\n")
	b.WriteString("**Always use `--output json` for all read commands** to get structured data with full IDs.\n\n")
	b.WriteString("### Read\n")
	b.WriteString("- `hira issue get <id> --output json` — Get full issue details (title, description, status, priority, assignee)\n")
	b.WriteString("- `hira issue list [--status X] [--priority X] [--assignee X] [--limit N] [--offset N] --output json` — List issues in workspace (default limit: 50; JSON output includes `total`, `has_more` — use offset to paginate when `has_more` is true)\n")
	b.WriteString("- `hira issue comment list <issue-id> [--limit N] [--offset N] [--since <RFC3339>] --output json` — List comments on an issue (supports pagination; includes id, parent_id for threading)\n")
	b.WriteString("- `hira workspace get --output json` — Get workspace details and context\n")
	b.WriteString("- `hira workspace members [workspace-id] --output json` — List workspace members (user IDs, names, roles)\n")
	b.WriteString("- `hira agent list --output json` — List agents in workspace\n")
	b.WriteString("- `hira repo checkout <url>` — Check out a repository into the working directory (creates a git worktree with a dedicated branch)\n")
	b.WriteString("- `hira issue runs <issue-id> --output json` — List all execution runs for an issue (status, timestamps, errors)\n")
	b.WriteString("- `hira issue run-messages <task-id> [--since <seq>] --output json` — List messages for a specific execution run (supports incremental fetch)\n")
	b.WriteString("- `hira attachment download <id> [-o <dir>]` — Download an attachment file locally by ID\n")
	b.WriteString("- `hira autopilot list [--status X] --output json` — List autopilots (scheduled/triggered agent automations) in the workspace\n")
	b.WriteString("- `hira autopilot get <id> --output json` — Get autopilot details including triggers\n")
	b.WriteString("- `hira autopilot runs <id> [--limit N] --output json` — List execution history for an autopilot\n\n")

	b.WriteString("### Write\n")
	b.WriteString("- `hira issue create --title \"...\" [--description \"...\"] [--priority X] [--assignee X] [--parent <issue-id>] [--status X]` — Create a new issue\n")
	b.WriteString("- `hira issue assign <id> --to <name>` — Assign an issue to a member or agent by name (use --unassign to remove assignee)\n")
	b.WriteString("- `hira issue comment add <issue-id> --content \"...\" [--parent <comment-id>]` — Post a comment (use --parent to reply to a specific comment)\n")
	b.WriteString("  - For content with special characters (backticks, quotes), pipe via stdin: `cat <<'COMMENT' | hira issue comment add <issue-id> --content-stdin`\n")
	b.WriteString("- `hira issue comment delete <comment-id>` — Delete a comment\n")
	b.WriteString("- `hira issue status <id> <status>` — Update issue status (todo, in_progress, in_review, done, blocked)\n")
	b.WriteString("- `hira issue update <id> [--title X] [--description X] [--priority X]` — Update issue fields\n")
	b.WriteString("- `hira autopilot create --title \"...\" --agent <name> --mode create_issue [--description \"...\"]` — Create an autopilot\n")
	b.WriteString("- `hira autopilot update <id> [--title X] [--description X] [--status active|paused]` — Update an autopilot\n")
	b.WriteString("- `hira autopilot trigger <id>` — Manually trigger an autopilot to run once\n")
	b.WriteString("- `hira autopilot delete <id>` — Delete an autopilot\n\n")

	// Inject available repositories section.
	if len(ctx.Repos) > 0 {
		b.WriteString("## Repositories\n\n")
		b.WriteString("The following code repositories are available in this workspace.\n")
		b.WriteString("Use `hira repo checkout <url>` to check out a repository into your working directory.\n\n")
		b.WriteString("| URL | Description |\n")
		b.WriteString("|-----|-------------|\n")
		for _, repo := range ctx.Repos {
			desc := repo.Description
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(&b, "| %s | %s |\n", repo.URL, desc)
		}
		b.WriteString("\nThe checkout command creates a git worktree with a dedicated branch. You can check out one or more repos as needed.\n\n")
	}

	b.WriteString("### Workflow\n\n")

	if ctx.ChatSessionID != "" {
		// Chat task: interactive assistant mode
		b.WriteString("**You are in chat mode.** A user is messaging you directly in a chat window.\n\n")
		b.WriteString("- Respond conversationally and helpfully to the user's message\n")
		b.WriteString("- You have full access to the `hira` CLI to look up issues, workspace info, members, agents, etc.\n")
		b.WriteString("- If asked about issues, use `hira issue list --output json` or `hira issue get <id> --output json`\n")
		b.WriteString("- If asked about the workspace, use `hira workspace get --output json`\n")
		b.WriteString("- If asked to perform actions (create issues, update status, etc.), use the appropriate CLI commands\n")
		b.WriteString("- If the task requires code changes, use `hira repo checkout <url>` to get the code first\n")
		b.WriteString("- Keep responses concise and direct\n\n")
	} else if ctx.TriggerCommentID != "" {
		// Comment-triggered: focus on reading and replying
		b.WriteString("**This task was triggered by a NEW comment.** Your primary job is to respond to THIS specific comment, even if you have handled similar requests before in this session.\n\n")
		fmt.Fprintf(&b, "1. Run `hira issue get %s --output json` to understand the issue context\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `hira issue comment list %s --output json` to read the conversation\n", ctx.IssueID)
		b.WriteString("   - If the output is very large or truncated, use pagination: `--limit 30` to get the latest 30 comments, or `--since <timestamp>` to fetch only recent ones\n")
		fmt.Fprintf(&b, "3. Find the triggering comment (ID: `%s`) and understand what is being asked — do NOT confuse it with previous comments\n", ctx.TriggerCommentID)
		b.WriteString("4. If the comment requests code changes or further work, do the work first\n")
		b.WriteString("5. **Post your reply as a comment — this step is mandatory.** Text in your terminal or run logs is NOT delivered to the user. ")
		b.WriteString(BuildCommentReplyInstructions(ctx.IssueID, ctx.TriggerCommentID))
		b.WriteString("6. Do NOT change the issue status unless the comment explicitly asks for it\n\n")
	} else {
		// Assignment-triggered: defer to agent Skills for workflow specifics.
		b.WriteString("You are responsible for managing the issue status throughout your work.\n\n")
		fmt.Fprintf(&b, "1. Run `hira issue get %s --output json` to understand your task\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `hira issue status %s in_progress`\n", ctx.IssueID)
		b.WriteString("3. Read comments for additional context or human instructions\n")
		b.WriteString("4. Follow your Skills and Agent Identity to complete the task (write code, investigate, etc.)\n")
		fmt.Fprintf(&b, "5. **Post your final results as a comment — this step is mandatory**: `hira issue comment add %s --content \"...\"`. Your results are only visible to the user if posted via this CLI call; text in your terminal or run logs is NOT delivered.\n", ctx.IssueID)
		fmt.Fprintf(&b, "6. When done, run `hira issue status %s in_review`\n", ctx.IssueID)
		fmt.Fprintf(&b, "7. If blocked, run `hira issue status %s blocked` and post a comment explaining why\n\n", ctx.IssueID)
	}

	// Business Knowledge — distinct from Skills. Always rendered when the
	// workspace has at least one published doc (the server includes a
	// digest even when retrieval yields zero hits), so agents in fresh
	// workspaces do not silently miss the KB.
	if strings.TrimSpace(ctx.KnowledgeContext) != "" {
		b.WriteString("## Business Knowledge\n\n")
		b.WriteString("Workspace này có một knowledge graph gồm SOP / brand / policy / org / process / glossary / product metadata. ")
		b.WriteString("Hệ thống ghi `.agent_context/knowledge_context.md` gồm hai phần: **(a) digest** — danh sách tất cả docs đã publish của workspace, và **(b) pre-loaded chunks** — top-K đoạn đã match với issue hiện tại (có thể rỗng nếu không match).\n\n")
		b.WriteString("**Cách dùng:**\n")
		b.WriteString("1. Đọc `.agent_context/knowledge_context.md` NGAY khi nhận task, trước khi gõ comment / code. Digest cho bạn bản đồ — pre-loaded chunks cho bạn nội dung đã match sẵn.\n")
		b.WriteString("2. Nếu pre-loaded chunks trống hoặc không đủ: tra digest để chọn doc phù hợp, rồi dùng MCP tool `search_knowledge` (nếu có trong danh sách tools) để query trực tiếp knowledge graph. Digest KHÔNG phải là toàn bộ — mỗi doc còn nhiều chunk bạn có thể truy cập.\n")
		b.WriteString("3. Áp dụng tone, quy trình, và đúng tên team / owner / policy được trích dẫn trong đó.\n")
		b.WriteString("4. Nếu yêu cầu mâu thuẫn với knowledge (vd khách xin hoàn tiền quá 30 ngày trong khi Policy là 30 ngày), comment nêu rõ mâu thuẫn và hỏi thêm TRƯỚC khi tự xử lý.\n")
		b.WriteString("5. Knowledge KHÁC Skills: Skills dạy bạn CÁCH làm (workflow, script); Knowledge nói cho bạn biết DOANH NGHIỆP LÀM GÌ (entity, quy trình, owner, ngôn ngữ).\n")
		b.WriteString("6. File này được re-render mỗi lần task dispatch — luôn là phiên bản mới nhất, không cache qua tasks.\n\n")
	}

	if len(ctx.AgentSkills) > 0 {
		b.WriteString("## Skills\n\n")
		switch provider {
		case "claude":
			// Claude discovers skills natively from .claude/skills/ — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "codex", "copilot", "opencode", "openclaw", "pi", "cursor":
			// Codex, Copilot, OpenCode, OpenClaw, Pi, and Cursor discover skills natively from their respective paths — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "gemini":
			// Gemini reads GEMINI.md directly; point it at the fallback skills dir.
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		default:
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		}
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Mentions\n\n")
	b.WriteString("When referencing issues or people in comments, use the mention format so they render as interactive links:\n\n")
	b.WriteString("- **Issue**: `[MUL-123](mention://issue/<issue-id>)` — renders as a clickable link to the issue\n")
	b.WriteString("- **Member**: `[@Name](mention://member/<user-id>)` — renders as a styled mention and sends a notification\n")
	b.WriteString("- **Agent**: `[@Name](mention://agent/<agent-id>)` — renders as a styled mention and re-triggers the agent\n\n")
	b.WriteString("⚠️ Agent and member mentions are **actions**, not text references: agent mentions enqueue a new task for the agent, and member mentions send a notification. ")
	b.WriteString("If you only want to refer to someone by name in prose (e.g. \"GPT-Boy is correct\"), write the plain name without the mention link.\n\n")
	b.WriteString("Use `hira issue list --output json` to look up issue IDs, and `hira workspace members --output json` for member IDs.\n\n")

	b.WriteString("## Attachments\n\n")
	b.WriteString("Issues and comments may include file attachments (images, documents, etc.).\n")
	b.WriteString("Use the download command to fetch attachment files locally:\n\n")
	b.WriteString("```\nhira attachment download <attachment-id>\n```\n\n")
	b.WriteString("This downloads the file to the current directory and prints the local path. Use `-o <dir>` to save elsewhere.\n")
	b.WriteString("After downloading, you can read the file directly (e.g. view an image, read a document).\n\n")

	b.WriteString("## Important: Always Use the `hira` CLI\n\n")
	b.WriteString("All interactions with Hira platform resources — including issues, comments, attachments, images, files, and any other platform data — **must** go through the `hira` CLI. ")
	b.WriteString("Do NOT use `curl`, `wget`, or any other HTTP client to access Hira URLs or APIs directly. ")
	b.WriteString("Hira resource URLs require authenticated access that only the `hira` CLI can provide.\n\n")
	b.WriteString("If you need to perform an operation that is not covered by any existing `hira` command, ")
	b.WriteString("do NOT attempt to work around it. Instead, post a comment mentioning the workspace owner to request the missing functionality.\n\n")

	b.WriteString("## Output\n\n")
	b.WriteString("⚠️ **Final results MUST be delivered via `hira issue comment add`.** The user does NOT see your terminal output, assistant chat text, or run logs — only comments on the issue. A task that finishes without a result comment is invisible to the user, even if the work itself was correct.\n\n")
	b.WriteString("Keep comments concise and natural — state the outcome, not the process.\n")
	b.WriteString("Good: \"Fixed the login redirect. PR: https://...\"\n")
	b.WriteString("Bad: \"1. Read the issue 2. Found the bug in auth.go 3. Created branch 4. ...\"\n")
	b.WriteString("When referencing issues in comments, **always** use the mention format `[MUL-123](mention://issue/<issue-id>)` so they render as clickable links.\n")

	return b.String()
}
