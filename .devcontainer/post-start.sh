#!/usr/bin/env bash
#
# Runs every time the devcontainer starts (including re-attach).
# Keep this fast: wait for Postgres and apply pending migrations.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Ensure go-installed tools (goose, etc.) are reachable.
export PATH="$(go env GOPATH)/bin:$PATH"

PG_HOST="${PG_HOST:-postgres}"
PG_USER="${PG_USER:-deuce}"
deadline=$(( $(date +%s) + 30 ))

echo "==> Waiting for Postgres at ${PG_HOST}"
while ! pg_isready -h "$PG_HOST" -U "$PG_USER" -q; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "WARNING: Postgres not reachable at ${PG_HOST} after 30s — skipping migrations." >&2
    exit 0
  fi
  sleep 1
done

echo "==> Applying migrations"
if ! ( cd server && make migrate ); then
  echo "WARNING: migrations failed — fix and re-run 'cd server && make migrate'." >&2
fi
