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

# Stage 2: fetch the CLI binaries the server shells out to.
#
# Deuce is not a self-contained binary at runtime: it drives `devpod` for every
# workspace operation, `docker` for the SSH proxy / SFTP / container-user lookup
# / devcontainer prebuild, and `git` for the files tab. A distroless runtime
# (the previous base) boots, migrates, and serves the SPA, then fails on the
# first session create with an exec-not-found. Both CLIs are fetched here so
# curl and the tarball never reach the runtime stage.
#
# Version pins are duplicated from .devcontainer/tool-versions.env rather than
# read from it: .dockerignore excludes .devcontainer from the build context, so
# the file is not readable here. Keep DEVPOD_VERSION in step with that file when
# bumping either one — a drift means the devcontainer and the shipped image
# drive DevPod differently.
FROM debian:bookworm-slim AS tools
ARG DEVPOD_VERSION=v0.6.15
ARG DOCKER_CLI_VERSION=29.7.0
# TARGETARCH is supplied by buildx. The release workflow builds linux/amd64
# only today; parameterizing here means adding linux/arm64 is a workflow
# change rather than a Dockerfile change.
ARG TARGETARCH
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    case "${TARGETARCH}" in \
        amd64) docker_arch="x86_64" ;; \
        arm64) docker_arch="aarch64" ;; \
        *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl --fail --silent --show-error --location \
        --output /tmp/docker.tgz \
        "https://download.docker.com/linux/static/stable/${docker_arch}/docker-${DOCKER_CLI_VERSION}.tgz"; \
    # The tarball carries the full engine; extract only the client. dockerd,
    # containerd and runc have no business in this image — the daemon is the
    # host's.
    tar --extract --file /tmp/docker.tgz --strip-components=1 --directory /usr/local/bin docker/docker; \
    curl --fail --silent --show-error --location \
        --output /usr/local/bin/devpod \
        "https://github.com/loft-sh/devpod/releases/download/${DEVPOD_VERSION}/devpod-linux-${TARGETARCH}"; \
    chmod 0755 /usr/local/bin/docker /usr/local/bin/devpod; \
    /usr/local/bin/docker --version; \
    /usr/local/bin/devpod version

# Stage 3: runtime.
#
# HOME is load-bearing, not cosmetic. Both DevPod state trees resolve through
# os.UserHomeDir(): the CLI's workspace records (which supply the container
# label the reconciler matches on) and the agent's cloned workspace content
# (which the files tab reads and runs git against). The SSH host key defaults
# under it too. A deployment mounts one host directory here so all three
# survive container replacement — and, for the socket-mounted topology, so the
# path string resolves to the same directory for both Deuce and the host
# daemon. See docs/plans/2026-07-31-001-feat-vm-deploy-topology-spike-plan.md.
FROM debian:bookworm-slim
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates \
        git \
    && rm -rf /var/lib/apt/lists/*
# UID 65532 matches the distroless `nonroot` user this image replaced, so any
# host directory already owned for the old image keeps working.
RUN groupadd --gid 65532 deuce \
    && useradd --uid 65532 --gid 65532 --home-dir /var/lib/deuce --create-home deuce
COPY --from=tools /usr/local/bin/docker /usr/local/bin/docker
COPY --from=tools /usr/local/bin/devpod /usr/local/bin/devpod
COPY --from=backend /out/deuce /usr/local/bin/deuce
ENV HOME=/var/lib/deuce
WORKDIR /var/lib/deuce
# 8080 HTTP (API + WS + embedded SPA), 2222 embedded SSH proxy for
# "Open in VS Code" (DEUCE_SSH_LISTEN_ADDR default).
EXPOSE 8080 2222
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/deuce"]
