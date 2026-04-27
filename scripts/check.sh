#!/usr/bin/env bash
set -euo pipefail

# ==========================================================================
# Verification pipeline: Go tests
# Usage: bash scripts/check.sh
# ==========================================================================

ENV_FILE="${ENV_FILE:-.env}"
if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_DB="${POSTGRES_DB:-hira}"
POSTGRES_USER="${POSTGRES_USER:-hira}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"

EXIT_CODE=0

cleanup() {
  echo ""
  if [ "$EXIT_CODE" -eq 0 ]; then
    echo "✓ All checks passed."
  else
    echo "✗ Checks FAILED."
  fi
  exit "$EXIT_CODE"
}
trap cleanup EXIT

# --------------------------------------------------------------------------
# Step 0: Ensure DB
# --------------------------------------------------------------------------
echo "==> Using env file: $ENV_FILE"
echo "==> Checking PostgreSQL..."
bash scripts/ensure-postgres.sh "$ENV_FILE"

# --------------------------------------------------------------------------
# Step 1: Migrations
# --------------------------------------------------------------------------
echo ""
echo "==> [1/2] Running database migrations..."
(cd server && go run ./cmd/migrate up) || { EXIT_CODE=1; exit 1; }

# --------------------------------------------------------------------------
# Step 2: Go tests
# --------------------------------------------------------------------------
echo ""
echo "==> [2/2] Go tests..."
(cd server && go test ./...) || { EXIT_CODE=1; exit 1; }
