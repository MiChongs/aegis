# syntax=docker/dockerfile:1
#
# 一起听微服务（services/listentogether）镜像。与主服务同一个 Go 模块、同一份 go.mod，
# 但只编译 ./services/listentogether/cmd/listen-together 这一个入口：依赖图里没有主服务的包，
# 镜像小、启动快，主服务的发版也不会让它重启。
#
#   docker build -f deploy/docker/listen-together.Dockerfile -t aegis-listen-together .
#
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /app
ARG GOPROXY=https://proxy.golang.org,direct
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0 GOFLAGS=-buildvcs=false

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY api ./api
COPY pkg ./pkg
COPY services ./services
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-lt-${TARGETARCH},sharing=locked \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o /usr/local/bin/listen-together ./services/listentogether/cmd/listen-together

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S lt && adduser -S lt -G lt
COPY --from=build /usr/local/bin/listen-together /usr/local/bin/listen-together
USER lt
# 8090 对外（WebSocket /ws、/healthz、/readyz）；8091 只在内网（Connect/gRPC 管理接口、/metrics）
EXPOSE 8090 8091
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8090/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/listen-together"]
