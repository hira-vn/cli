# Hira CLI

**Your next 10 hires won't be human.**

The `hira` CLI connects your machine to Hira — authenticate, manage workspaces, and run the local agent daemon that executes AI tasks.

[![Release](https://github.com/hira-vn/cli/actions/workflows/release.yml/badge.svg)](https://github.com/hira-vn/cli/actions/workflows/release.yml)

[Website](https://hira.vn) · [App](https://app.hira.vn) · [Issues](https://github.com/hira-vn/cli/issues)

---

## Install

### macOS / Linux — Homebrew (recommended)

```bash
brew install hira-vn/tap/cli
```

Upgrade:

```bash
brew upgrade hira-vn/tap/cli
```

### macOS / Linux — install script

```bash
curl -fsSL https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.sh | bash
```

Uses Homebrew when available, otherwise downloads the binary directly from GitHub Releases.

### npm

```bash
npm install -g @hira-vn/cli
```

### Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.ps1 | iex
```

### Self-host (install CLI + provision server)

```bash
curl -fsSL https://raw.githubusercontent.com/hira-vn/cli/main/scripts/install.sh | bash -s -- --with-server
```

Requires Docker. Clones this repo, starts the server via Docker Compose, then installs the CLI.

---

## Quick Start

```bash
# Connect to Hira Cloud — configure, log in, start daemon (one step)
hira setup

# For a self-hosted server:
hira setup self-host
```

`hira setup` does three things: saves the server URL, opens your browser for authentication, and starts the local agent daemon.

---

## Usage

### Global flags

These flags work on every command:

| Flag | Env var | Description |
|------|---------|-------------|
| `--server-url` | `HIRA_SERVER_URL` | Override the backend server URL |
| `--workspace-id` | `HIRA_WORKSPACE_ID` | Override the active workspace |
| `--profile` | — | Use a named config profile (isolates config, daemon state, and workspaces) |

Authentication token can also be set via `HIRA_TOKEN` instead of running `hira login`.

---

### Setup & Authentication

```bash
# One-step setup: configure → login → start daemon
hira setup                  # Hira Cloud (hira.vn)
hira setup cloud            # Same as above, explicit
hira setup self-host        # Self-hosted server (prompts for URL)
hira setup self-host \
  --server-url https://api.internal.co \
  --app-url    https://app.internal.co

# Login / logout
hira login                  # Opens browser for OAuth
hira login --token          # Paste a personal access token instead
hira auth status            # Show current auth state
hira auth logout            # Remove stored token
```

---

### Daemon

The daemon runs locally, polls for tasks, and executes them using AI agent CLIs on your PATH (`claude`, `codex`, `opencode`, etc.).

```bash
hira daemon start           # Start in the background
hira daemon start --foreground               # Run in current terminal
hira daemon start --max-concurrent-tasks 3  # Limit parallel tasks
hira daemon start --poll-interval 10s       # Custom poll interval
hira daemon start --agent-timeout 30m       # Per-task timeout

hira daemon stop            # Stop the running daemon
hira daemon restart         # Stop + start
hira daemon status          # Show status and detected runtimes
hira daemon status --output json

hira daemon logs            # Last 50 lines
hira daemon logs -f         # Follow (tail -f style)
hira daemon logs -n 200     # Show last 200 lines
```

---

### Issues

```bash
# List & search
hira issue list
hira issue list --status in_progress
hira issue list --priority high --assignee claude
hira issue list --project <project-id> --limit 100
hira issue search "auth bug"
hira issue search "refactor" --include-closed

# Get details
hira issue get <id>
hira issue get <id> --output json

# Create
hira issue create --title "Fix login timeout"
hira issue create \
  --title "Add dark mode" \
  --description "Support prefers-color-scheme" \
  --priority medium \
  --assignee claude \
  --project <project-id>

# Update
hira issue update <id> --title "New title"
hira issue update <id> --status done
hira issue update <id> --priority urgent --assignee alice

# Change status directly
hira issue status <id> in_progress
hira issue status <id> done

# Assign
hira issue assign <id> --to claude
hira issue assign <id> --unassign

# Attachments
hira issue create --title "Bug report" --attachment ./screenshot.png
hira issue create --title "Bug report" \
  --attachment ./screenshot.png \
  --attachment ./logs.txt

# Comments
hira issue comment list <issue-id>
hira issue comment add  <issue-id> --content "Looks good"
hira issue comment add  <issue-id> --content-stdin < notes.txt
hira issue comment add  <issue-id> --content "reply" --parent <comment-id>
hira issue comment delete <comment-id>

# Subscribers
hira issue subscriber list   <issue-id>
hira issue subscriber add    <issue-id>              # subscribe yourself
hira issue subscriber add    <issue-id> --user alice
hira issue subscriber remove <issue-id> --user alice

# Execution history
hira issue runs         <issue-id>      # list agent runs
hira issue run-messages <task-id>       # messages for a specific run
hira issue run-messages <task-id> --since 42
```

---

### Projects

```bash
hira project list
hira project list --status active

hira project get <id>

hira project create --title "Q3 Roadmap"
hira project create \
  --title "Backend infra" \
  --description "..." \
  --icon "🔧" \
  --lead alice \
  --status active

hira project update <id> --title "New name"
hira project update <id> --lead claude --icon "🤖"

hira project status <id> completed

hira project delete <id>
```

---

### Agents

Agents are AI workers you define in Hira. Each agent is backed by a runtime (a local machine running `hira daemon`).

```bash
# List & inspect
hira agent list
hira agent list --include-archived
hira agent get <id>

# Create
hira agent create \
  --name "Claude Worker" \
  --runtime-id <runtime-id> \
  --instructions "You are a senior Go engineer..."
hira agent create \
  --name "o3 Coder" \
  --runtime-id <runtime-id> \
  --custom-args '["--model", "o3"]' \
  --max-concurrent-tasks 3 \
  --visibility workspace

# Update
hira agent update <id> --name "Renamed"
hira agent update <id> --max-concurrent-tasks 10
hira agent update <id> --instructions "Updated prompt..."

# Archive / restore
hira agent archive <id>
hira agent restore <id>

# Tasks
hira agent tasks <id>

# Skills
hira agent skills list <agent-id>
hira agent skills set  <agent-id> --skill-ids id1,id2,id3
```

---

### Runtimes

Runtimes represent machines running `hira daemon`. Managed from any machine once authenticated.

```bash
hira runtime list
hira runtime usage    <runtime-id>              # token usage (last 90 days)
hira runtime usage    <runtime-id> --days 30
hira runtime activity <runtime-id>              # hourly task chart
hira runtime ping     <runtime-id>              # check connectivity
hira runtime ping     <runtime-id> --wait       # wait for response
hira runtime update   <runtime-id> --target-version v0.1.5
hira runtime update   <runtime-id> --target-version v0.1.5 --wait
```

---

### Autopilots

Autopilots are scheduled or triggered agent automations — they create and assign issues automatically on a cron schedule.

```bash
# List & inspect
hira autopilot list
hira autopilot list --status active
hira autopilot get <id>

# Create
hira autopilot create \
  --title "Daily standup reporter" \
  --description "Write a standup summary from recent commits" \
  --agent claude \
  --mode create_issue

hira autopilot create \
  --title "Weekly dependency audit" \
  --agent claude \
  --mode create_issue \
  --priority medium \
  --project <project-id> \
  --issue-title-template "Dep audit {{.Date}}"

# Update & delete
hira autopilot update <id> --status paused
hira autopilot update <id> --agent new-agent --priority high
hira autopilot delete <id>

# Manual trigger
hira autopilot trigger <id>

# Execution history
hira autopilot runs <id>
hira autopilot runs <id> --limit 50

# Schedule triggers (cron)
hira autopilot trigger-add <autopilot-id> \
  --cron "0 9 * * 1-5" \
  --timezone "Asia/Ho_Chi_Minh" \
  --label "Weekdays 9am"

hira autopilot trigger-update <autopilot-id> <trigger-id> \
  --cron "0 10 * * *"
hira autopilot trigger-update <autopilot-id> <trigger-id> \
  --enabled=false         # pause this trigger

hira autopilot trigger-delete <autopilot-id> <trigger-id>
```

---

### Workspaces

```bash
hira workspace list
hira workspace get                  # active workspace
hira workspace get <workspace-id>
hira workspace members
hira workspace members <workspace-id>
```

---

### Config

```bash
hira config show                              # show current config
hira config set server_url  https://api.hira.vn
hira config set app_url     https://app.hira.vn
hira config set workspace_id <id>
```

Config is stored in `~/.hira/config.json`. Use `--profile` to maintain multiple configs (e.g. `--profile dev`, `--profile staging`).

---

### MCP server

Exposes Hira's knowledge graph as a [Model Context Protocol](https://modelcontextprotocol.io) stdio server for use inside agent CLIs:

```bash
hira mcp knowledge
```

Add to your agent's MCP config (e.g. Claude Code's `.claude/settings.json`):

```json
{
  "mcpServers": {
    "hira": {
      "command": "hira",
      "args": ["mcp", "knowledge"]
    }
  }
}
```

---

### Other commands

```bash
hira version              # show CLI version
hira version --output json

hira update               # update to the latest release
```

---

## Output formats

Most commands support `--output table` (default, human-readable) or `--output json` (machine-readable). Use `json` for scripting:

```bash
hira issue list --output json | jq '.[].id'
hira agent list --output json | jq '.[] | select(.status=="active")'
```

---

## What is Hira?

Hira turns coding agents into real teammates. Assign issues to an agent like you would assign to a colleague — they pick up the work, write code, report blockers, and update statuses autonomously.

The daemon auto-detects AI agent CLIs on your PATH (`claude`, `codex`, `opencode`, `openclaw`, `hermes`, `gemini`, `pi`, `cursor-agent`) and registers them as available runtimes.

---

## Architecture

This repo contains the **Go backend and CLI binary**. The frontend lives separately.

```
server/
  cmd/hira/      # hira CLI binary
  cmd/server/    # HTTP/WebSocket API server
  cmd/migrate/   # Database migration runner
  internal/      # Business logic (auth, daemon, handlers, realtime)
  pkg/           # Shared packages (agent protocol, DB layer, knowledge)
  migrations/    # PostgreSQL migrations
```

| Component | Stack |
|-----------|-------|
| CLI | Go (Cobra) |
| API Server | Go (Chi router, gorilla/websocket) |
| Database | PostgreSQL 17 with pgvector |
| Agent Runtime | Local daemon executing Claude Code, Codex, OpenCode, etc. |

---

## Development

**Prerequisites:** [Go](https://go.dev/) v1.24+, [Docker](https://www.docker.com/)

```bash
git clone https://github.com/hira-vn/cli.git
cd cli
cp .env.example .env
make dev       # auto-setup: DB, migrations, server
```

```bash
make server           # Run Go API server (port 8080)
make hira ARGS="..."  # Run CLI (e.g. make hira ARGS="daemon status")
make build            # Build all binaries to server/bin/
make test             # Run Go tests
make migrate-up       # Run pending migrations
make migrate-down     # Roll back last migration
make sqlc             # Regenerate sqlc DB layer after editing SQL queries
```

### Self-host (Docker Compose)

```bash
make selfhost        # Start full stack
make selfhost-stop   # Stop all services
```

---

## Release

Releases are triggered by pushing a version tag:

```bash
git tag v0.x.x
git push origin v0.x.x
```

GitHub Actions runs Go tests → GoReleaser builds multi-platform binaries → publishes to GitHub Releases, updates the [Homebrew tap](https://github.com/hira-vn/homebrew-tap), and publishes `@hira-vn/cli` to npm.

Bump the patch version by default (`v0.1.12` → `v0.1.13`) unless a minor/major change warrants otherwise.

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
