# syntax=docker/dockerfile:1.7

# Web UI: built first so the Go binary can embed web/dist.
# Platform-independent output: always built natively on the build host.
FROM --platform=$BUILDPLATFORM oven/bun:1.2.9 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY api/ /src/api/
COPY web/ ./
RUN bun run build

# Cross-compiles for the target platform instead of running Go under emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/ekin ./cmd/ekin \
 && mkdir -p /out/data

# Worker: the same binary plus the tools it drives — Chromium and ffmpeg
# (render), poppler (PDF text), uv + Python (okur / EMA / Kokoro speech) and the
# Claude Code CLI (authoring agent). Build with --target worker.
FROM debian:bookworm-slim AS worker
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates chromium chromium-headless-shell ffmpeg poppler-utils git curl python3 nodejs npm tini \
 && npm install -g @anthropic-ai/claude-code \
 && npm cache clean --force \
 && curl -LsSf https://astral.sh/uv/install.sh | env UV_INSTALL_DIR=/usr/local/bin INSTALLER_NO_MODIFY_PATH=1 sh \
 && apt-get purge -y npm && apt-get autoremove -y && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 65532 --create-home --home-dir /home/ekin ekin
COPY --from=build /out/ekin /usr/local/bin/ekin
COPY tools/speech /opt/ekin/speech
COPY --from=build --chown=65532:65532 /out/data /data
# Model weights and the uv cache live on the data volume so they download
# once, not on every image update.
ENV EKIN_DATA_DIR=/data \
    EKIN_LOG_FORMAT=json \
    EKIN_CHROME=/usr/lib/chromium/headless_shell \
    EKIN_SPEECH_DIR=/opt/ekin/speech \
    EKIN_SPEECH_MODELS=/data/cache/speech \
    UV_CACHE_DIR=/data/cache/uv \
    UV_PYTHON_INSTALL_DIR=/data/cache/python \
    HOME=/home/ekin
USER 65532:65532
VOLUME /data
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/ekin"]
CMD ["worker"]

# The API and the scheduler need nothing but the static binary.
FROM gcr.io/distroless/static-debian12:nonroot AS serve
COPY --from=build /out/ekin /ekin
# A named volume copies this directory's ownership on first use, so the
# nonroot user can write the database.
COPY --from=build --chown=65532:65532 /out/data /data
ENV EKIN_DATA_DIR=/data \
    EKIN_HTTP_ADDR=0.0.0.0:8080 \
    EKIN_LOG_FORMAT=json
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/ekin"]
CMD ["serve"]
