.PHONY: dev build test migrate migrate-down migrate-status generate tidy

DATABASE_URL ?= postgres://deuce:deuce@localhost:5432/deuce?sslmode=disable

dev:
	air

build:
	go build -o bin/deuce .

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
