# syntax=docker/dockerfile:1
#
# BuildKit 的 cache mount 是这份文件里最要紧的一行：
# ./cmd/server 的依赖图有 1800+ 个包，没有 GOCACHE 时**每次**构建都要全量编译一遍，
# 改一行代码和改一整个模块的代价完全一样。挂上之后只重编真正变了的那些包。
# 缓存不进镜像层，因此 go mod download 与 go build 必须挂同一组 cache。

# ── 构建阶段 ──
# --platform=$BUILDPLATFORM：构建多架构镜像时留在原生架构上交叉编译。
# 不写这一行的话 buildx 会用 QEMU 模拟目标架构跑整个 Go 工具链，慢一个数量级。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /app

# 模块代理可按地域覆盖（中国大陆构建加速）：
#   docker compose build --build-arg GOPROXY=https://goproxy.cn,direct server
ARG GOPROXY=https://proxy.golang.org,direct
# BuildKit 自动注入，无需在 compose 里声明；经典构建器下为空，等同于按宿主架构编译
ARG TARGETOS
ARG TARGETARCH
# -buildvcs=false：.dockerignore 排除了 .git，省掉一次注定失败的版本戳探测
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0 GOFLAGS=-buildvcs=false

# 依赖层独立缓存：go.mod/go.sum 未变更时跳过整个依赖下载
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

COPY . .
# go-build 缓存按目标架构隔离：多架构构建共用一份会互相冲刷，反而更慢
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-${TARGETARCH},sharing=locked \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /usr/local/bin/aegis-server ./cmd/server

# ── 运行阶段 ──
FROM alpine:3.22
# 中日韩字形，让中文/日文/韩文的支付凭证开箱即用。它是这个镜像里最大的一块，
# 实测占近一半体积（370MB → 197MB），因此做成开关而不是写死：
#
#   docker build --build-arg WITH_CJK_FONTS=0 ...
#
# 关掉之后中日韩凭证**自动降级成英文，不会报错** —— 拉丁字形内嵌在二进制里，不依赖此包。
# 也就是说这个开关只有出海外文凭证的部署才该动，判断依据是"要不要出中日韩凭证"，
# 而不是"镜像大不大"。
# 也可以改为挂载自备字体并配 PAYMENT_RECEIPT_FONT_PATH（须是 .ttf/.ttc，不支持 .otf）。
ARG WITH_CJK_FONTS=1
# ca-certificates: 出站 TLS（支付/OAuth 上游）；tzdata: 本地化时区（支付凭证按用户时区打印时间）
# wget 由 busybox 提供（健康检查用）
#
# 建账号与建目录并进这一层：放在 COPY 之后再 `chown -R /app` 会多走一遍全部迁移文件，
# 且多产生一个与上一层等大的镜像层。下面的 COPY 直接带 --chown，落地即是终态属主。
RUN apk add --no-cache ca-certificates tzdata \
    && if [ "${WITH_CJK_FONTS}" = "1" ]; then apk add --no-cache font-noto-cjk; fi \
    && addgroup -S aegis && adduser -S aegis -G aegis \
    && mkdir -p /app/data \
    && chown -R aegis:aegis /app
WORKDIR /app
COPY --from=build /usr/local/bin/aegis-server /usr/local/bin/aegis-server
COPY --chown=aegis:aegis migrations ./migrations
USER aegis
EXPOSE 8088
CMD ["/usr/local/bin/aegis-server"]
