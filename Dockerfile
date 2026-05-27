# syntax=docker/dockerfile:1.10
#
# This Dockerfile assumes the Vite frontend has already been built into ./dist/
# in the build context. The release GitHub Actions workflow builds the frontend
# on its own runner step (`npm ci && npm run build`) and the dist/ output is
# present when this Dockerfile is invoked.
#
# Locally: run `npm run build` at repo root before `docker build .`, OR use the
# `make embed-dist` Makefile target from server/.
#
# Rationale: keeping the frontend build outside Docker lets CI fail fast on a
# typecheck/build break without spinning up the buildx pipeline, and lets the
# Docker image stay single-language (Go-only) for faster iteration.

# Stage 1: build the Go server with the frontend dist/ embedded.
FROM golang:1.25-bookworm AS backend
WORKDIR /src
ARG VERSION=dev
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server ./server
# dist/ MUST exist in the build context. The release workflow's npm-build job
# produces it; locally, `npm run build` at repo root does the same.
COPY dist ./server/internal/web/dist
RUN cd server && CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o /out/deuce .

# Stage 2: minimal runtime. distroless/static-debian12:nonroot is correct for
# CGO_ENABLED=0 static Go binaries: no glibc, no shell, no package manager.
# Runs as UID 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/deuce /deuce
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/deuce"]
