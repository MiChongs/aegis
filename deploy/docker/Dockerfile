# ── 构建阶段 ──
FROM golang:1.26-alpine AS build
WORKDIR /app

# 模块代理可按地域覆盖（中国大陆构建加速）：
#   docker compose build --build-arg GOPROXY=https://goproxy.cn,direct server
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0

# 依赖层独立缓存：go.mod/go.sum 未变更时跳过整个依赖下载
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /usr/local/bin/aegis-server ./cmd/server

# ── 运行阶段 ──
FROM alpine:3.22
# ca-certificates: 出站 TLS（支付/OAuth 上游）；tzdata: 本地化时区（支付凭证按用户时区打印时间）；
# font-noto-cjk: 中日韩字形，让中文/日文/韩文的支付凭证开箱即用。
#   镜像因此增大约 50MB；只出英文凭证的部署可以去掉它 ——
#   拉丁字形内嵌在二进制里，不依赖此包，去掉后中日韩凭证会自动降级成英文而不是报错。
#   也可以改为挂载自备字体并配 PAYMENT_RECEIPT_FONT_PATH（须是 .ttf/.ttc，不支持 .otf）。
# wget 由 busybox 提供（健康检查用）
RUN apk add --no-cache ca-certificates tzdata font-noto-cjk \
    && addgroup -S aegis && adduser -S aegis -G aegis
WORKDIR /app
COPY --from=build /usr/local/bin/aegis-server /usr/local/bin/aegis-server
COPY --chown=aegis:aegis migrations ./migrations
# 运行期可写目录（账单导出等）
RUN mkdir -p /app/data && chown -R aegis:aegis /app
USER aegis
EXPOSE 8088
CMD ["/usr/local/bin/aegis-server"]
