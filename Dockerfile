# Multi-stage build for metacubexd-server-go (All-in-One).
#
# Build args (all default to empty = official sources):
#   METACUBEXD_VERSION / MIHOMO_VERSION  — pin UI/kernel release tags
#   GOPROXY       — Go module proxy (e.g. https://goproxy.cn,direct)
#   APK_MIRROR    — Alpine apk mirror (e.g. mirrors.ustc.edu.cn)
#
# China users typically set both in one place — either via CLI:
#   docker compose build \
#     --build-arg GOPROXY=https://goproxy.cn,direct \
#     --build-arg APK_MIRROR=mirrors.ustc.edu.cn
# or in docker-compose.yml (build.args section).

# ─── stage 1: build the Go server ───────────────────────────────────────────
FROM golang:1.26.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
ARG GOPROXY=
RUN GOPROXY=${GOPROXY} go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY *.go ./
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/metacubexd-server ./cmd/metacubexd-server

# ─── stage 2: assemble runtime with UI + mihomo ─────────────────────────────
FROM alpine:3.24 AS assets
ARG METACUBEXD_VERSION=v1.270.0
ARG MIHOMO_VERSION=v1.19.27
ARG TARGETARCH
ARG APK_MIRROR=

RUN if [ -n "$APK_MIRROR" ]; then \
      sed -i "s/dl-cdn.alpinelinux.org/$APK_MIRROR/g" /etc/apk/repositories; \
    fi && \
    apk add --no-cache curl tar gzip ca-certificates

RUN mkdir -p /ui && \
    curl -fsSL -o /tmp/ui.tgz \
      "https://github.com/MetaCubeX/metacubexd/releases/download/${METACUBEXD_VERSION}/compressed-dist.tgz" && \
    tar xzf /tmp/ui.tgz -C /ui && \
    rm /tmp/ui.tgz

RUN mkdir -p /bin && \
    arch="${TARGETARCH:-amd64}" && \
    curl -fsSL -o /tmp/mihomo.gz \
      "https://github.com/MetaCubeX/mihomo/releases/download/${MIHOMO_VERSION}/mihomo-linux-${arch}-${MIHOMO_VERSION}.gz" && \
    gunzip /tmp/mihomo.gz && \
    chmod +x /tmp/mihomo && \
    mv /tmp/mihomo /bin/mihomo

# ─── stage 3: minimal runtime ───────────────────────────────────────────────
FROM alpine:3.24
ARG APK_MIRROR=
# tzdata + ca-certificates: TLS + timezone. su-exec: drop-privilege helper.
RUN if [ -n "$APK_MIRROR" ]; then \
        sed -i "s/dl-cdn.alpinelinux.org/$APK_MIRROR/g" /etc/apk/repositories; \
    fi && \
    apk add --no-cache tzdata ca-certificates util-linux

COPY --from=build /out/metacubexd-server /app/metacubexd-server
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
COPY --from=assets /ui /ui
COPY --from=assets /bin/mihomo /usr/local/bin/mihomo

RUN mkdir -p /data /app && \
    chmod +x /app/docker-entrypoint.sh /app/metacubexd-server

ENV CONTROL_PORT=8080 \
    CLASH_API_PORT=9090 \
    MIXED_PORT=7890 \
    DATA_DIR=/data \
    MIHOMO_BIN=/usr/local/bin/mihomo \
    UI_DIST=/ui

EXPOSE 8080 7890

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/metacubexd-server"]
