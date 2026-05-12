FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

# System packages the language features don't cover.
# - postgresql-client: psql + pg_isready, used by post-start.sh to wait for the DB
# - build-essential / make: required by `make dev` and a few Go tool builds
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        git \
        make \
        postgresql-client \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
