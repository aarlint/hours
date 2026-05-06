# syntax=docker/dockerfile:1.6

# ---- Frontend build ----
FROM node:20-bookworm AS frontend
WORKDIR /web
COPY internal/web/package.json internal/web/package-lock.json ./
RUN npm ci
COPY internal/web/ ./
RUN npm run build

# ---- Go build ----
# Wails on Linux requires GTK + WebKit dev headers even when only the
# --serve HTTP path is used at runtime, because main.go imports the Wails
# package unconditionally.
FROM golang:1.25-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    libgtk-3-dev \
    libwebkit2gtk-4.1-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Bring in the built frontend assets so the //go:embed in internal/web/embed.go succeeds.
COPY --from=frontend /web/dist ./internal/web/dist
ENV CGO_ENABLED=1
RUN go build -tags webkit2_41 -ldflags="-s -w -X main.version=docker" -o /out/hours-mcp .

# ---- Runtime ----
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libgtk-3-0 \
    libwebkit2gtk-4.1-0 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1000 hours \
    && mkdir -p /home/hours/.hours \
    && chown -R hours:hours /home/hours
USER hours
WORKDIR /home/hours
COPY --from=builder /out/hours-mcp /usr/local/bin/hours-mcp
EXPOSE 7878
CMD ["hours-mcp", "--serve", "--addr", ":7878"]
