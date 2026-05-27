.PHONY: dev build test migrate migrate-down migrate-status generate tidy embed-dist release-build

DATABASE_URL ?= postgres://deuce:deuce@localhost:5432/deuce?sslmode=disable

dev:
	air

# Build the Vite frontend at repo root and copy the output into
# internal/web/dist/ so `go:embed all:dist` has real files to embed.
# The Dockerfile does the equivalent at production build time.
embed-dist:
	cd .. && npm run build
	rm -rf internal/web/dist
	cp -r ../dist internal/web/dist

build:
	go build -o bin/deuce .

# Build the binary the same way the release Dockerfile does: CGO off, static,
# stripped, with the version injected via ldflags. Run `make embed-dist` first
# so the //go:embed directive has the real Vite build, not just .gitkeep.
#
# Usage: make release-build VERSION=v0.1.0
release-build:
	@: $${VERSION?VERSION env var is required (e.g. VERSION=v0.1.0)}
	CGO_ENABLED=0 GOOS=linux go build \
		-trimpath \
		-ldflags="-s -w -X main.Version=$(VERSION)" \
		-o bin/deuce .

test:
	go test ./...

tidy:
	go mod tidy

generate:
	sqlc generate

migrate:
	goose -dir internal/db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir internal/db/migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir internal/db/migrations postgres "$(DATABASE_URL)" status
