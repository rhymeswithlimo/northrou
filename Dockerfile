# Multi-stage build for the Northrou server.
# The runtime image ships ffmpeg and tesseract from the distro, so Northrou's
# system-ffmpeg fallback is used and nothing is downloaded at first run.

# --- build stage ---
FROM golang:1.26-bookworm AS build
WORKDIR /src

# Cache modules first.
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download

COPY backend ./backend
ARG VERSION=docker
RUN cd backend && CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/rhymeswithlimo/northrou/backend/internal/buildinfo.Version=${VERSION}" \
    -o /out/northrou ./cmd/northrou

# --- runtime stage ---
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg tesseract-ocr ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/northrou /usr/local/bin/northrou

# Config and data live on volumes; media is mounted read-only by the user.
ENV NORTHROU_CONFIG_DIR=/config \
    NORTHROU_DATA_DIR=/data
VOLUME ["/config", "/data"]
EXPOSE 8674

# Run in the foreground (not as an OS service) inside the container.
ENTRYPOINT ["northrou", "serve", "--no-browser", "--config", "/config/config.toml"]
