FROM node:22.21.1-alpine3.22 AS web-builder
WORKDIR /src/web
RUN corepack enable && corepack prepare pnpm@9.5.0 --activate
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/ ./
RUN pnpm build

FROM golang:1.25.0-alpine3.22 AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/transportation-platform ./cmd/platform

FROM alpine:3.22 AS runtime
RUN addgroup -S -g 10001 platform && adduser -S -D -H -u 10001 -G platform platform \
    && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /out/transportation-platform /usr/local/bin/transportation-platform
COPY --from=web-builder /src/web/dist /app/web/dist
ENV APP_ENV=production PORT=8080 WEB_DIR=/app/web/dist
STOPSIGNAL SIGTERM
USER platform
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider "http://127.0.0.1:${PORT}/healthz" || exit 1
ENTRYPOINT ["/usr/local/bin/transportation-platform"]
