# =============================================================================
# archistrator server — multi-stage Go build → small non-root runtime image.
#
# BUILD CONTEXT: the `archistrator` repo root (parent of server/).
# Platform modules (github.com/davidmarne/archistrator-platform/*) are public
# on GitHub; no auth is required and they resolve via the default GOPROXY.
#
#     docker buildx build --platform=linux/amd64 \
#       -f server/Dockerfile \
#       -t archistrator-server:dev .
#
# (go.work is NOT used in Docker — GOWORK=off so resolution is purely via
# go.mod + the published v0.1.0 tags on archistrator-platform.)
# =============================================================================

# syntax=docker/dockerfile:1.7
# --- Stage 1: build -----------------------------------------------------------
FROM golang:1.25.4-bookworm AS build

# Install ca-certificates so the module proxy HTTPS calls work inside the build.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Copy go.mod + go.sum first to maximise layer-cache reuse across source-only
# changes — module download only reruns when the dependency manifest changes.
COPY server/go.mod server/go.sum ./server/

WORKDIR /src/server

# Download all dependencies. Platform modules are public — no token needed.
RUN GOWORK=off go mod download

# Copy the full server source and build.
COPY server/ /src/server/

# Static, stripped binary — no cgo, so it runs on a distroless/scratch-class base.
ENV CGO_ENABLED=0 GOOS=linux

RUN GOWORK=off go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# --- Stage 2: runtime ---------------------------------------------------------
# Distroless static: minimal, no shell, non-root by default (the :nonroot tag
# runs as uid 65532). CA certs are included for outbound TLS (Temporal cloud /
# IdP), matching the gtd image's non-root + small-runtime convention.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="archistrator-server" \
      org.opencontainers.image.description="archistrator system-design server (UC1 webClient + embedded Temporal worker)" \
      org.opencontainers.image.source="https://github.com/davidmarne/archistrator" \
      org.opencontainers.image.vendor="davidmarne"

COPY --from=build /server /server

# Matches config.go ARCHISTRATOR_LISTEN_ADDR default (:8080).
EXPOSE 8080

USER 65532:65532
ENTRYPOINT ["/server"]
