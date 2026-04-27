# CLI and Agent Daemon Guide

The `hira` CLI connects your local machine to Hira. It handles authentication, workspace management, issue tracking, and runs the agent daemon that executes AI tasks locally.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install hira-vn/tap/cli
```

### Build from Source

```bash
git clone https://github.com/hira-vn/cli.git
cd hira
make build
cp server/bin/hira /usr/local/bin/hira
```

### Update

```bash
brew upgrade hira-vn/tap/cli
```

For install script or manual installs, use:

```bash
hira update
```

`hira update` auto-detects your installation method and upgrades accordingly.

## Quick Start

```bash
# One-command setup: configure, authenticate, and start the daemon
hira setup

# For self-hosted (local) deployments:
hira setup self-host
```

Or step by step:

```bash
# 1. Authenticate (opens browser for login)
hira login

# 2. Start the agent daemon
hira daemon start

# 3. Done — agents in your watched workspaces can now execute tasks on your machine
```

`hira login` automatically discovers all workspaces you belong to and adds them to the daemon watch list.

## Authentication

### Browser Login

```bash
hira login
```

Opens your browser for OAuth authentication, creates a 90-day personal access token, and auto-configures your workspaces.

### Token Login

```bash
hira login --token
```

Authenticate by pasting a personal access token directly. Useful for headless environments.

### Check Status

```bash
hira auth status
```

Shows your current server, user, and token validity.

### Logout

```bash
hira auth logout
```

Removes the stored authentication token.

## Agent Daemon

The daemon is the local agent runtime. It detects available AI CLIs on your machine, registers them with the Hira server, and executes tasks when agents are assigned work.

### Start

```bash
hira daemon start
```

By default, the daemon runs in the background and logs to `~/.hira/daemon.log`.

To run in the foreground (useful for debugging):

```bash
hira daemon start --foreground
```

### Stop

```bash
hira daemon stop
```

### Status

```bash
hira daemon status
hira daemon status --output json
```

Shows PID, uptime, detected agents, and watched workspaces.

### Logs

```bash
hira daemon logs              # Last 50 lines
hira daemon logs -f           # Follow (tail -f)
hira daemon logs -n 100       # Last 100 lines
```

### Supported Agents

The daemon auto-detects these AI CLIs on your PATH:

| CLI | Command | Description |
|-----|---------|-------------|
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | `claude` | Anthropic's coding agent |
| [Codex](https://github.com/openai/codex) | `codex` | OpenAI's coding agent |
| OpenCode | `opencode` | Open-source coding agent |
| OpenClaw | `openclaw` | Open-source coding agent |
| Hermes | `hermes` | Nous Research coding agent |
| Gemini | `gemini` | Google's coding agent |
| [Pi](https://pi.dev/) | `pi` | Pi coding agent |
| [Cursor Agent](https://cursor.com/) | `cursor-agent` | Cursor's headless coding agent |

You need at least one installed. The daemon registers each detected CLI as an available runtime.

### How It Works

1. On start, the daemon detects installed agent CLIs and registers a runtime for each agent in each watched workspace
2. It polls the server at a configurable interval (default: 3s) for claimed tasks
3. When a task arrives, it creates an isolated workspace directory, spawns the agent CLI, and streams results back
4. Heartbeats are sent periodically (default: 15s) so the server knows the daemon is alive
5. On shutdown, all runtimes are deregistered

### Configuration

Daemon behavior is configured via flags or environment variables:

| Setting | Flag | Env Variable | Default |
|---------|------|--------------|---------|
| Poll interval | `--poll-interval` | `HIRA_DAEMON_POLL_INTERVAL` | `3s` |
| Heartbeat interval | `--heartbeat-interval` | `HIRA_DAEMON_HEARTBEAT_INTERVAL` | `15s` |
| Agent timeout | `--agent-timeout` | `HIRA_AGENT_TIMEOUT` | `2h` |
| Max concurrent tasks | `--max-concurrent-tasks` | `HIRA_DAEMON_MAX_CONCURRENT_TASKS` | `20` |
| Daemon ID | `--daemon-id` | `HIRA_DAEMON_ID` | hostname |
| Device name | `--device-name` | `HIRA_DAEMON_DEVICE_NAME` | hostname |
| Runtime name | `--runtime-name` | `HIRA_AGENT_RUNTIME_NAME` | `Local Agent` |
| Workspaces root | — | `HIRA_WORKSPACES_ROOT` | `~/hira_workspaces` |

Agent-specific overrides:

| Variable | Description |
|----------|-------------|
| `HIRA_CLAUDE_PATH` | Custom path to the `claude` binary |
| `HIRA_CLAUDE_MODEL` | Override the Claude model used |
| `HIRA_CODEX_PATH` | Custom path to the `codex` binary |
| `HIRA_CODEX_MODEL` | Override the Codex model used |
| `HIRA_OPENCODE_PATH` | Custom path to the `opencode` binary |
| `HIRA_OPENCODE_MODEL` | Override the OpenCode model used |
| `HIRA_OPENCLAW_PATH` | Custom path to the `openclaw` binary |
| `HIRA_OPENCLAW_MODEL` | Override the OpenClaw model used |
| `HIRA_HERMES_PATH` | Custom path to the `hermes` binary |
| `HIRA_HERMES_MODEL` | Override the Hermes model used |
| `HIRA_GEMINI_PATH` | Custom path to the `gemini` binary |
| `HIRA_GEMINI_MODEL` | Override the Gemini model used |
| `HIRA_PI_PATH` | Custom path to the `pi` binary |
| `HIRA_PI_MODEL` | Override the Pi model used |
| `HIRA_CURSOR_PATH` | Custom path to the `cursor-agent` binary |
| `HIRA_CURSOR_MODEL` | Override the Cursor Agent model used |

### Self-Hosted Server

When connecting to a self-hosted Hira instance, the easiest approach is:

```bash
# One command — configures for localhost, authenticates, starts daemon
hira setup self-host

# Or for on-premise with custom domains:
hira setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

Or configure manually:

```bash
# Set URLs individually
hira config set server_url http://localhost:8080
hira config set app_url http://localhost:3000

# For production with TLS:
# hira config set server_url https://api.example.com
# hira config set app_url https://app.example.com

hira login
hira daemon start
```

### Profiles

Profiles let you run multiple daemons on the same machine — for example, one for production and one for a staging server.

```bash
# Set up a staging profile
hira setup self-host --profile staging --server-url https://api-staging.example.com --app-url https://staging.example.com

# Start its daemon
hira daemon start --profile staging

# Default profile runs separately
hira daemon start
```

Each profile gets its own config directory (`~/.hira/profiles/<name>/`), daemon state, health port, and workspace root.

## Workspaces

### List Workspaces

```bash
hira workspace list
```

Watched workspaces are marked with `*`. The daemon only processes tasks for watched workspaces.

### Watch / Unwatch

```bash
hira workspace watch <workspace-id>
hira workspace unwatch <workspace-id>
```

### Get Details

```bash
hira workspace get <workspace-id>
hira workspace get <workspace-id> --output json
```

### List Members

```bash
hira workspace members <workspace-id>
```

## Issues

### List Issues

```bash
hira issue list
hira issue list --status in_progress
hira issue list --priority urgent --assignee "Agent Name"
hira issue list --limit 20 --output json
```

Available filters: `--status`, `--priority`, `--assignee`, `--project`, `--limit`.

### Get Issue

```bash
hira issue get <id>
hira issue get <id> --output json
```

### Create Issue

```bash
hira issue create --title "Fix login bug" --description "..." --priority high --assignee "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--priority`, `--assignee`, `--parent`, `--project`, `--due-date`.

### Update Issue

```bash
hira issue update <id> --title "New title" --priority urgent
```

### Assign Issue

```bash
hira issue assign <id> --to "Lambda"
hira issue assign <id> --unassign
```

### Change Status

```bash
hira issue status <id> in_progress
```

Valid statuses: `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`.

### Comments

```bash
# List comments
hira issue comment list <issue-id>

# Add a comment
hira issue comment add <issue-id> --content "Looks good, merging now"

# Reply to a specific comment
hira issue comment add <issue-id> --parent <comment-id> --content "Thanks!"

# Delete a comment
hira issue comment delete <comment-id>
```

### Subscribers

```bash
# List subscribers of an issue
hira issue subscriber list <issue-id>

# Subscribe yourself to an issue
hira issue subscriber add <issue-id>

# Subscribe another member or agent by name
hira issue subscriber add <issue-id> --user "Lambda"

# Unsubscribe yourself
hira issue subscriber remove <issue-id>

# Unsubscribe another member or agent
hira issue subscriber remove <issue-id> --user "Lambda"
```

Subscribers receive notifications about issue activity (new comments, status changes, etc.). Without `--user`, the command acts on the caller.

### Execution History

```bash
# List all execution runs for an issue
hira issue runs <issue-id>
hira issue runs <issue-id> --output json

# View messages for a specific execution run
hira issue run-messages <task-id>
hira issue run-messages <task-id> --output json

# Incremental fetch (only messages after a given sequence number)
hira issue run-messages <task-id> --since 42 --output json
```

The `runs` command shows all past and current executions for an issue, including running tasks. The `run-messages` command shows the detailed message log (tool calls, thinking, text, errors) for a single run. Use `--since` for efficient polling of in-progress runs.

## Projects

Projects group related issues (e.g. a sprint, an epic, a workstream). Every project
belongs to a workspace and can optionally have a lead (member or agent).

### List Projects

```bash
hira project list
hira project list --status in_progress
hira project list --output json
```

Available filters: `--status`.

### Get Project

```bash
hira project get <id>
hira project get <id> --output json
```

### Create Project

```bash
hira project create --title "2026 Week 16 Sprint" --icon "🏃" --lead "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--icon`, `--lead`.

### Update Project

```bash
hira project update <id> --title "New title" --status in_progress
hira project update <id> --lead "Lambda"
```

Flags: `--title`, `--description`, `--status`, `--icon`, `--lead`.

### Change Status

```bash
hira project status <id> in_progress
```

Valid statuses: `planned`, `in_progress`, `paused`, `completed`, `cancelled`.

### Delete Project

```bash
hira project delete <id>
```

### Associating Issues with Projects

Use the `--project` flag on `issue create` / `issue update` to attach an issue to a
project, or on `issue list` to filter issues by project:

```bash
hira issue create --title "Login bug" --project <project-id>
hira issue update <issue-id> --project <project-id>
hira issue list --project <project-id>
```

## Setup

```bash
# One-command setup for Hira Cloud: configure, authenticate, and start the daemon
hira setup

# For local self-hosted deployments
hira setup self-host

# Custom ports
hira setup self-host --port 9090 --frontend-port 4000

# On-premise with custom domains
hira setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

`hira setup` configures the CLI, opens your browser for authentication, and starts the daemon — all in one step. Use `hira setup self-host` to connect to a self-hosted server instead of Hira Cloud.

## Configuration

### View Config

```bash
hira config show
```

Shows config file path, server URL, app URL, and default workspace.

### Set Values

```bash
hira config set server_url https://api.example.com
hira config set app_url https://app.example.com
hira config set workspace_id <workspace-id>
```

## Autopilot Commands

Autopilots are scheduled/triggered automations that dispatch agent tasks (either by creating an issue or by running an agent directly).

### List Autopilots

```bash
hira autopilot list
hira autopilot list --status active --output json
```

### Get Autopilot Details

```bash
hira autopilot get <id>
hira autopilot get <id> --output json   # includes triggers
```

### Create / Update / Delete

```bash
hira autopilot create \
  --title "Nightly bug triage" \
  --description "Scan todo issues and prioritize." \
  --agent "Lambda" \
  --mode create_issue

hira autopilot update <id> --status paused
hira autopilot update <id> --description "New prompt"
hira autopilot delete <id>
```

`--mode` currently only accepts `create_issue` (creates a new issue on each run and assigns it to the agent). The server data model also defines `run_only`, but the daemon task path doesn't yet resolve a workspace for runs without an issue, so it's not exposed by the CLI. `--agent` accepts either a name or UUID.

### Manual Trigger

```bash
hira autopilot trigger <id>            # Fires the autopilot once, returns the run
```

### Run History

```bash
hira autopilot runs <id>
hira autopilot runs <id> --limit 50 --output json
```

### Schedule Triggers

```bash
hira autopilot trigger-add <autopilot-id> --cron "0 9 * * 1-5" --timezone "America/New_York"
hira autopilot trigger-update <autopilot-id> <trigger-id> --enabled=false
hira autopilot trigger-delete <autopilot-id> <trigger-id>
```

Only cron-based `schedule` triggers are currently exposed via the CLI. The data model also defines `webhook` and `api` kinds, but there is no server endpoint that fires them yet, so they're not surfaced here.

## Other Commands

```bash
hira version              # Show CLI version and commit hash
hira update               # Update to latest version
hira agent list           # List agents in the current workspace
```

## Output Formats

Most commands support `--output` with two formats:

- `table` — human-readable table (default for list commands)
- `json` — structured JSON (useful for scripting and automation)

```bash
hira issue list --output json
hira daemon status --output json
```
