# syntax=docker/dockerfile:1
#
# 风控平台 Go 服务镜像（docs/risk-platform.md）：risk-decision、risk-admin、risk-ingest 三个二进制同一个镜像，
# compose 里用 command 区分。与主服务同一个 Go 模块，但只编译 ./services/risk/cmd/...。
#
# zen-go 是 Rust 引擎的 CGO 绑定，静态库按 glibc 编译 —— 所以这里用 Debian 而不是 Alpine，
# 运行阶段也是 Debian slim（同一个 glibc 大版本）。
#
#   docker build -f deploy/docker/risk.Dockerfile -t aegis-risk --build-arg GOPROXY=https://goproxy.cn,direct .
#
FROM golang:1.26-bookworm AS build
WORKDIR /app
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=dev
ENV GOPROXY=${GOPROXY} CGO_ENABLED=1 GOFLAGS=-buildvcs=false

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY api ./api
COPY pkg ./pkg
COPY services ./services
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-risk,sharing=locked \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ \
      ./services/risk/cmd/risk-decision ./services/risk/cmd/risk-admin ./services/risk/cmd/risk-ingest

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -r -g 10001 risk && useradd -r -u 10001 -g risk -d /var/lib/aegis-risk risk \
    && mkdir -p /var/lib/aegis-risk && chown risk:risk /var/lib/aegis-risk
COPY --from=build /out/ /usr/local/bin/
USER risk
ENV RISK_SPOOL_DIR=/var/lib/aegis-risk
# 8890 Kitex（risk-decision）、8891 HTTP（risk-admin）、8892 健康检查（risk-decision）
EXPOSE 8890 8891 8892
CMD ["/usr/local/bin/risk-decision"]
