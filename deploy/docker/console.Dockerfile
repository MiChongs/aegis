# syntax=docker/dockerfile:1
#
# 管理前端镜像。**构建上下文是 aegis-console/**（与 Zeabur 上该服务的 RootDirectory 一致），
# 但文件本身放在 deploy/docker/ 下：
#
#   docker build -f deploy/docker/console.Dockerfile \
#     --build-arg AEGIS_API_BACKEND=http://aegis-server:8088 \
#     -t aegis-console aegis-console
#
# 不叫 aegis-console/Dockerfile 是刻意的：zbpack 见到构建根下有 Dockerfile 就会
# 从「Next.js 自动识别」切到「docker 计划」，而那条路上如果没人把 AEGIS_API_BACKEND
# 作为 build arg 传进来，下面的默认值会被烘进产物 —— 控制台反代到它自己。
# 这种切换在 Dashboard 上看不出来，放在这里就不会误触。
#
# 产物走 Next 的 standalone 输出：只带真正被追踪到的依赖，而完整的 node_modules 是 1.4GB。

ARG NODE_VERSION=24-alpine

# ── 公共底座 ──
FROM node:${NODE_VERSION} AS base
# pnpm 版本的唯一事实源是 package.json 的 packageManager 字段，corepack 直接读它。
# 在这里再钉一个版本号就会有两处配置，而它们迟早对不上。
ENV PNPM_HOME=/pnpm \
    PATH=/pnpm:$PATH \
    COREPACK_ENABLE_DOWNLOAD_PROMPT=0 \
    NEXT_TELEMETRY_DISABLED=1
RUN corepack enable pnpm
WORKDIR /app

# ── 依赖 ──
# 单独一层：源码改动不触发重装。pnpm store 挂 cache mount，
# 连 lockfile 变了也只下增量的那几个包。
FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN --mount=type=cache,target=/pnpm/store,sharing=locked \
    pnpm install --frozen-lockfile

# ── 构建 ──
FROM base AS build

# ⚠ 后端地址是**构建期**烘进产物的，不是运行期读的。
# next.config.ts 的 rewrites() 在 build 时求值，结果序列化进 .next/routes-manifest.json，
# 因此换后端地址必须重新构建镜像，改容器环境变量没有任何作用。
#
# 而且它必须填后端的**内网**地址。填公网域名的话这一跳会绕出公网再回来，
# 途中的 CDN / 网关如实写下「连接方是控制台」—— 那是个公网地址、后端不信任它，
# 于是全站每个用户的每个请求都收敛成控制台的出口地址，限流 / 封禁 / 地理风控 /
# 审计集体失准。详见 docs/client-ip.md。
#
#   docker build --build-arg AEGIS_API_BACKEND=http://aegis-server:8088 ...
ARG AEGIS_API_BACKEND=http://127.0.0.1:8088
ENV AEGIS_API_BACKEND=${AEGIS_API_BACKEND} \
    NEXT_OUTPUT=standalone

COPY --from=deps /app/node_modules ./node_modules
COPY . .
# Turbopack 的文件系统缓存挂成 cache mount：重复构建镜像时编译阶段不必从零开始。
# prebuild 里的 clean-next.mjs 刻意跳过 .next/cache，两者配合才有效。
RUN --mount=type=cache,target=/app/.next/cache,sharing=locked \
    pnpm build

# ── 运行 ──
# 刻意不从 base 起：那一层装了 corepack / pnpm，而运行时镜像里不该有包管理器。
FROM node:${NODE_VERSION} AS runner
WORKDIR /app
# node 镜像自带 uid 1000 的 node 用户，直接用，不必再造一个账号多加一层。
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

# standalone 产物不含这两样，Next 要求自行搬运：
#   .next/static —— 带 hash 的静态资源
#   public       —— 含自托管的 monaco（23MB，脚本编辑器按需加载）
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public

USER node
EXPOSE 3000

# --import 这一段不是可选的：它在本进程的 HTTP 服务器边界把直连对端追加进
# X-Forwarded-For，后端的限流 / 封禁 / 地理风控 / 审计全建立在那个地址上。
# 少了它容器照样起、页面照样开，只是全站客户端 IP 都变成控制台自己 —— 功能上看不出来。
# 启动日志里那行「▲ 客户端 IP 透传已启用」是唯一的自检线索。
CMD ["node", "--import=./scripts/forwarded-headers-preload.mjs", "server.js"]
