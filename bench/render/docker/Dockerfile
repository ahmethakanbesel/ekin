# VPS proxy: Debian + distro Chromium (+ headless shell) + ffmpeg. The bench dir is mounted at /bench.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium chromium-headless-shell ffmpeg python3 procps ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /bench
