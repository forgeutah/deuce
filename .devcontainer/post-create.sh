#!/usr/bin/env bash
#
# One-time setup after the devcontainer is created.
# Idempotent — safe to re-run manually (`bash .devcontainer/post-create.sh`).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Pinned tool versions live in one place.
# shellcheck disable=SC1091
set -a; . .devcontainer/tool-versions.env; set +a

# Make `go install` targets reachable in this script and in future shells.
export PATH="$(go env GOPATH)/bin:$PATH"

echo "==> Installing Go tools (goose ${GOOSE_VERSION}, sqlc ${SQLC_VERSION}, air ${AIR_VERSION})"
go install "github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}"
go install "github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION}"
go install "github.com/air-verse/air@${AIR_VERSION}"

echo "==> Ensuring DevPod CLI ${DEVPOD_VERSION}"
if ! devpod version 2>/dev/null | grep -qF "${DEVPOD_VERSION}"; then
  arch="$(uname -m)"
  case "$arch" in
    x86_64)         devpod_arch="amd64" ;;
    aarch64|arm64)  devpod_arch="arm64" ;;
    *) echo "Unsupported arch for DevPod install: $arch" >&2; exit 1 ;;
  esac
  curl --fail --silent --show-error --location \
    --output /tmp/devpod \
    "https://github.com/loft-sh/devpod/releases/download/${DEVPOD_VERSION}/devpod-linux-${devpod_arch}"
  sudo install -m 0755 /tmp/devpod /usr/local/bin/devpod
  rm -f /tmp/devpod
fi

echo "==> Preparing server/.env"
if [ ! -f server/.env ]; then
  cp server/.env.example server/.env
  echo "    Created server/.env from server/.env.example."
  echo "    Fill in GITHUB_TOKEN and ANTHROPIC_API_KEY for full functionality."
else
  echo "    server/.env already exists — leaving it alone."
fi

echo "==> go mod download"
( cd server && go mod download )

echo "==> npm install (frontend)"
# Don't abort the whole setup on npm hiccups — leave the container usable for backend work.
npm install || echo "WARNING: npm install failed — run it manually before starting the frontend." >&2

cat <<EOF

Devcontainer ready.

  Backend:   cd server && make dev    (Go on :8080, hot reload via air)
  Frontend:  npm run dev              (Vite on :5173, proxies /api and /ws)

Postgres is reachable at hostname 'postgres:5432' (see DATABASE_URL).
EOF
