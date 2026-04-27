# Repository Guidelines for AI Agents

This file provides guidance to AI agents (Claude Code, Codex, etc.) when working with code in this repository.

> Read **CLAUDE.md** first — it has the full architecture, commands, and coding rules.

## What This Repo Is

Go backend + CLI binary for Hira. No frontend code lives here.

```
server/cmd/hira/     hira CLI binary (Cobra)
server/cmd/server/   HTTP/WebSocket API server
server/cmd/migrate/  Migration runner
server/internal/     Business logic
server/pkg/          Shared packages (sqlc DB layer, agent protocol, knowledge)
server/migrations/   PostgreSQL schema migrations (immutable)
scripts/             Dev, install, and CI helpers
```

## Key Invariants

- **Migrations are immutable.** Never edit or delete `server/migrations/*.sql`. Add a new file.
- **All queries filter by `workspace_id`.** Multi-tenancy is enforced at the query level.
- **Agent assignees are polymorphic.** `assignee_type` + `assignee_id` — can be a member or an agent.
- **sqlc is the DB layer.** Edit SQL in `server/pkg/db/queries/`, then run `make sqlc` to regenerate Go code. Never hand-edit generated files in `server/pkg/db/generated/`.

## Commands Most Useful for Agents

```bash
# Build and verify
cd server && go build ./...     # Fast compile check
make test                       # Full test run (requires DB)
make check                      # Migrations + go test

# Codegen
make sqlc                       # Regenerate after editing SQL queries

# Run a single test
cd server && go test ./internal/handler/ -run TestWorkspaceMember -v
```

## Common Patterns

### Adding a new CLI command

1. Create `server/cmd/hira/cmd_<name>.go`
2. Add `Use`, `Short`, `RunE` fields on the `cobra.Command`
3. Register it in `init()` of that file: `rootCmd.AddCommand(newCmd)` or as a subcommand
4. Follow the pattern in adjacent files for flag parsing, API calls, and output formatting

### Adding a new API endpoint

1. Add the route in `server/cmd/server/router.go`
2. Create the handler in `server/internal/handler/`
3. Add SQL queries in `server/pkg/db/queries/`, run `make sqlc`
4. Add a migration in `server/migrations/` if the schema changes

### Adding a migration

```bash
# Name format: NNN_description.up.sql / NNN_description.down.sql
# NNN = next sequential number (check existing files)
touch server/migrations/052_add_foo.up.sql
touch server/migrations/052_add_foo.down.sql
```

### Env vars

Daemon behavior is configured via `HIRA_*` env vars. See `server/internal/daemon/config.go` for the full list. CLI config lives at `~/.hira/config.json` (or `~/.hira/profiles/<name>/config.json` for named profiles).
