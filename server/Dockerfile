# ---- build stage ----
# CGO is off so the result is a static binary that needs no libc at runtime.
# This is why the SQLite driver is the pure-Go modernc.org build.
FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies are copied first so the module download layer is reused
# whenever only application code has changed.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/audiora ./cmd/audiora

# ---- runtime stage ----
FROM alpine:3.20

# ffmpeg is the only large dependency, and it is genuinely required: it
# probes metadata, extracts cover art and transcodes.
# tini reaps the ffmpeg children the transcoder spawns, which otherwise
# accumulate as zombies on a long-running server.
RUN apk add --no-cache \
      ffmpeg \
      ca-certificates \
      tzdata \
      tini \
    && addgroup -S audiora \
    && adduser -S -G audiora -u 10001 audiora

COPY --from=build /out/audiora /usr/local/bin/audiora

# The data directory holds the database, cover art and the transcode cache.
# It is a volume, so the ownership only matters at first creation.
RUN mkdir -p /data && chown -R audiora:audiora /data
VOLUME ["/data"]

ENV LISTEN_ADDR=:8080 \
    DATA_PATH=/data

USER audiora
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/audiora"]
