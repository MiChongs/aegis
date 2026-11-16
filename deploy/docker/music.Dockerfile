# syntax=docker/dockerfile:1
#
# 音乐个性化微服务（services/music，docs/music-personalization.md）镜像：一个镜像三个入口
# （music-rec / music-ingest / music-trainer），compose 里用 entrypoint 选。与主服务同一个 Go 模块，
# 但只编译 services/music 的入口：依赖图里没有主服务的包，主服务发版不会让它们重启。
#
#   docker build -f deploy/docker/music.Dockerfile -t aegis-music .
#
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /app
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
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
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-music-${TARGETARCH},sharing=locked \
    for svc in music-rec music-ingest music-trainer; do \
      GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
      go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/$svc ./services/music/cmd/$svc || exit 1; \
    done

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S music && adduser -S music -G music
COPY --from=build /out/ /usr/local/bin/
USER music
# music-rec：8096（Connect / gRPC、管理端 HTTP、/metrics、探针，只在内网）；ingest / trainer：8097（/metrics、探针）
EXPOSE 8096 8097
ENTRYPOINT ["/usr/local/bin/music-rec"]
