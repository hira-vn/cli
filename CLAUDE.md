# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Project Context

This repo is the **Go backend and CLI binary** for Hira — an AI-native task management platform where coding agents are first-class teammates.

**What lives here:**
- `server/cmd/hira/` — the `hira` CLI binary (Cobra)
- `server/cmd/server/` — the HTTP/WebSocket API server (Chi + gorilla/websocket)
- `server/cmd/migrate/` — database migration runner
- `server/internal/` — business logic (auth, daemon, handlers, realtime, services)
- `server/pkg/` — shared packages (agent protocol, sqlc DB layer, knowledge)
- `server/migrations/` — PostgreSQL migrations (immutable — never rewrite history)
- `scripts/` — dev, install, and CI helper scripts
- `.goreleaser.yml` + `.github/workflows/release.yml` — release pipeline

**What does NOT live here:** the Next.js frontend. It's a separate repo.

## Architecture

```
server/
  cmd/hira/      CLI entry point — Cobra commands for auth, daemon, issues, etc.
  cmd/server/    API server — Chi router, WebSocket hub, background workers
  cmd/migrate/   Migration runner — up/down via golang-migrate
  internal/
    auth/        JWT, cookies, OAuth flow
    cli/         CLI config (~/.hira/config.json), profile management
    daemon/      Local agent runtime — process management, workspace isolation
    events/      In-process event bus
    handler/     HTTP route handlers
    middleware/  Auth middleware, workspace scoping
    realtime/    WebSocket hub
    service/     Business services (task, email, autopilot, storage)
  pkg/
    agent/       Agent protocol types
    db/          sqlc-generated DB layer + queries
    knowledge/   Knowledge context rendering for agents
    protocol/    Shared protocol types
```

**Multi-tenancy:** all queries filter by `workspace_id`. Membership checks gate access. `X-Workspace-ID` header routes requests to the correct workspace.

**Agent lifecycle:** daemon polls the server for claimed tasks → spawns the agent CLI in an isolated workspace dir → streams results back via WebSocket.

## Commands

```bash
# Dev (auto-setup env/DB/migrations, start server)
make dev

# Explicit setup and run
make setup           # Start DB, run migrations
make start           # Start API server (port 8080)
make stop            # Stop server process

# Individual services
make server          # Run API server only
make daemon          # Run local daemon (profile=local)
make hira ARGS="..." # Run CLI (e.g. make hira ARGS="daemon status")
make build           # Build all binaries to server/bin/

# Testing
make test            # Run Go tests (requires DB)
make check           # Full check: migrations + go test

# Database
make migrate-up      # Run pending migrations
make migrate-down    # Roll back last migration
make sqlc            # Regenerate sqlc code after editing server/pkg/db/queries/

# Self-host (Docker Compose)
make selfhost        # Start full stack
make selfhost-stop   # Stop all services

# Run a single Go test
cd server && go test ./internal/handler/ -run TestName
```

## Coding Rules

- Go code follows standard conventions (`gofmt`, `go vet`).
- Keep comments in code **English only**.
- Prefer existing patterns over new abstractions.
- Do not add backwards-compatibility layers, fallbacks, or dual-write logic unless explicitly asked.
- If something is being replaced and the product is not yet live, remove the old path instead of keeping both.
- Avoid broad refactors unless required by the task.

## Migrations

`server/migrations/*.sql` files are **immutable**. Never rewrite or delete migration files — they are the historical record of schema changes. Add a new migration instead.

## CLI Release

1. Create a tag on `main`: `git tag v0.x.x`
2. Push: `git push origin v0.x.x`
3. GitHub Actions triggers `release.yml`: runs Go tests → GoReleaser builds multi-platform binaries → publishes to GitHub Releases + updates the Homebrew tap

Bump the patch version by default (`v0.1.12` → `v0.1.13`).

## Commit Rules

- Atomic commits grouped by logical intent.
- Conventional format: `feat(scope)`, `fix(scope)`, `refactor(scope)`, `docs`, `test(scope)`, `chore(scope)`.
