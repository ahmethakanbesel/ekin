# syntax=docker/dockerfile:1.7

# Web UI: built first so the Go binary can embed web/dist.
FROM oven/bun:1.2.9 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY api/ /src/api/
COPY web/ ./
RUN bun run build

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/kurgu ./cmd/kurgu \
 && mkdir -p /out/data

# The API and the scheduler need nothing but the static binary. The worker
# will get its own image once it runs Chromium, ffmpeg, Claude Code and TTS.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kurgu /kurgu
# A named volume copies this directory's ownership on first use, so the
# nonroot user can write the database.
COPY --from=build --chown=65532:65532 /out/data /data
ENV KURGU_DATA_DIR=/data \
    KURGU_HTTP_ADDR=0.0.0.0:8080 \
    KURGU_LOG_FORMAT=json
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/kurgu"]
CMD ["serve"]
