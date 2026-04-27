# Hira CLI

**Your next 10 hires won't be human.**

The `hira` CLI connects your machine to Hira — authenticate, manage workspaces, and run the local agent daemon that executes AI tasks.

[![Release](https://github.com/hira-vn/cli/actions/workflows/release.yml/badge.svg)](https://github.com/hira-vn/cli/actions/workflows/release.yml)

[Website](https://hira.vn) · [App](https://app.hira.vn) · [CLI Reference](CLI_AND_DAEMON.md) · [Install Guide](CLI_INSTALL.md)

---

## Quick Install

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

The script uses Homebrew when available, otherwise downloads the binary directly from GitHub Releases.

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
# Connect to Hira Cloud — configure, log in, start daemon
hira setup

# Or for a self-hosted server:
hira setup self-host
```

`hira setup` does three things in one step: saves the server URL, opens your browser for authentication, and starts the local agent daemon.

---

## CLI Reference

| Command | Description |
|---------|-------------|
| `hira setup` | Configure for Hira Cloud, log in, start daemon |
| `hira setup self-host` | Configure for a self-hosted server, log in, start daemon |
| `hira login` | Authenticate (opens browser) |
| `hira auth status` | Show current auth state |
| `hira daemon start` | Start the local agent runtime |
| `hira daemon status` | Check daemon status and detected agents |
| `hira daemon logs` | View daemon logs |
| `hira issue list` | List issues in the current workspace |
| `hira issue create` | Create a new issue |
| `hira workspace list` | List workspaces |
| `hira version` | Show CLI version |
| `hira update` | Update to the latest version |

See [CLI_AND_DAEMON.md](CLI_AND_DAEMON.md) for the full command reference including daemon config, issue/project/autopilot management, and output formats.

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
# Clone and start
git clone https://github.com/hira-vn/cli.git
cd cli

# Copy env and start (auto-setup: DB, migrations, server)
cp .env.example .env
make dev
```

```bash
make server          # Run Go API server (port 8080)
make hira ARGS="..."  # Run CLI (e.g. make hira ARGS="daemon status")
make build           # Build all binaries to server/bin/
make test            # Run Go tests
make migrate-up      # Run pending migrations
make migrate-down    # Roll back last migration
make sqlc            # Regenerate sqlc DB layer after editing SQL queries
```

### Self-host (Docker Compose)

```bash
make selfhost        # Start full stack via docker-compose.selfhost.yml
make selfhost-stop   # Stop all services
```

### Worktree support

Multiple checkouts share one PostgreSQL container. Each worktree gets its own DB and ports:

```bash
make worktree-env    # Generate .env.worktree with unique DB/ports
make setup-worktree
make start-worktree
```

---

## Release

Releases are triggered by pushing a version tag:

```bash
git tag v0.x.x
git push origin v0.x.x
```

GitHub Actions runs Go tests → GoReleaser builds multi-platform binaries → publishes to GitHub Releases and updates the [Homebrew tap](https://github.com/hira-vn/homebrew-tap).

Bump the patch version by default (`v0.1.12` → `v0.1.13`) unless a minor/major change warrants otherwise.

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
