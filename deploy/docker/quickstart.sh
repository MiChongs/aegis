#!/usr/bin/env bash
# ══════════════════════════════════════════════════════════════════════════
#  Aegis 一键部署（Linux / macOS，容器化全栈）
#
#  做的事：环境检查 → 生成强随机凭据 .env → 构建镜像 → 启动全栈
#          → 自动迁移 → 等待健康 → 打印访问信息与凭据
#
#  用法：
#    ./deploy/docker/quickstart.sh              # 一键全栈
#    ./deploy/docker/quickstart.sh --infra      # 仅基础设施（本机 go run 开发）
#    ./deploy/docker/quickstart.sh --down       # 停止并移除容器（保留数据卷）
#    ./deploy/docker/quickstart.sh --status     # 查看栈状态
#    GOPROXY_CN=1 ./deploy/docker/quickstart.sh # 中国大陆构建加速
# ══════════════════════════════════════════════════════════════════════════
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
ENV_FILE="$ROOT_DIR/.env"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
step() { echo -e "${CYAN}▸${NC} $1"; }
ok()   { echo -e "${GREEN}✓${NC} $1"; }
warn() { echo -e "${YELLOW}!${NC} $1"; }
die()  { echo -e "${RED}✗${NC} $1"; exit 1; }

# ── 环境检查 ──
command -v docker >/dev/null 2>&1 || die "未检测到 Docker，请先安装：https://docs.docker.com/get-docker/"
docker info >/dev/null 2>&1 || die "Docker 守护进程未运行，请先启动 Docker"
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  die "未检测到 docker compose 插件或 docker-compose"
fi

compose() { "${COMPOSE[@]}" --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

# ── 子命令 ──
case "${1:-}" in
  --down)
    [ -f "$ENV_FILE" ] || touch "$ENV_FILE"
    step "停止 Aegis 栈（数据卷保留）..."
    compose --profile app --profile full --profile ui down
    ok "已停止"; exit 0 ;;
  --status)
    [ -f "$ENV_FILE" ] || touch "$ENV_FILE"
    compose --profile app --profile full --profile ui ps; exit 0 ;;
esac

MODE="app"
[ "${1:-}" = "--infra" ] && MODE="infra"

# ── 生成 .env（已存在则不覆盖，保障幂等与既有凭据安全） ──
rand() { (openssl rand -hex "$1" 2>/dev/null || head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n') | head -c $(( $1 * 2 )); }

if [ -f "$ENV_FILE" ]; then
  ok "检测到已有 .env，沿用现有配置（如需重新生成请先删除或备份）"
else
  step "首次部署：基于 .env.example 生成强随机凭据..."
  [ -f "$ROOT_DIR/.env.example" ] || die "缺少 .env.example 模板"
  cp "$ROOT_DIR/.env.example" "$ENV_FILE"

  JWT_SECRET="$(rand 32)"
  ADMIN_TOKEN="$(rand 24)"
  ADMIN_PASS="Aegis@$(rand 6)"
  DB_PASS="$(rand 16)"

  # 跨平台 sed（GNU 与 BSD 兼容：写临时文件后替换）。
  # DSN 写宿主机视角（localhost:15432）便于本机 go run 开发；
  # 容器化运行时由 compose 的 environment 自动覆盖为服务名地址。
  tmp="$ENV_FILE.tmp"
  sed \
    -e "s|^JWT_SECRET=.*|JWT_SECRET=${JWT_SECRET}|" \
    -e "s|^ADMIN_API_TOKEN=.*|ADMIN_API_TOKEN=${ADMIN_TOKEN}|" \
    -e "s|^ADMIN_BOOTSTRAP_PASSWORD=.*|ADMIN_BOOTSTRAP_PASSWORD=${ADMIN_PASS}|" \
    -e "s|^POSTGRES_DSN=.*|POSTGRES_DSN=postgres://aegis:${DB_PASS}@localhost:15432/aegis?sslmode=disable|" \
    "$ENV_FILE" > "$tmp" && mv "$tmp" "$ENV_FILE"

  # 编排层变量（compose 插值使用，与 DSN 中的密码保持一致）
  {
    echo ""
    echo "# ── 容器编排变量（quickstart 自动生成） ──"
    echo "AEGIS_DB_USER=aegis"
    echo "AEGIS_DB_PASSWORD=${DB_PASS}"
    echo "AEGIS_DB_NAME=aegis"
    echo "TEMPORAL_DB_PASSWORD=$(rand 12)"
  } >> "$ENV_FILE"
  chmod 600 "$ENV_FILE" 2>/dev/null || true
  ok "已生成 $ENV_FILE（数据库/JWT/管理员凭据均为强随机值）"
fi

# ── 兼容既有部署的 Temporal 外部数据卷（新机器自动创建，幂等） ──
docker volume create docker_temporal_postgres_data >/dev/null

# ── 构建与启动 ──
BUILD_ARGS=()
[ "${GOPROXY_CN:-0}" = "1" ] && BUILD_ARGS+=(--build-arg GOPROXY=https://goproxy.cn,direct) && step "使用 goproxy.cn 加速构建"

if [ "$MODE" = "infra" ]; then
  step "启动核心基础设施（postgres / redis / nats）..."
  compose up -d
  ok "基础设施已就绪，可在宿主机执行：go run ./cmd/server"
else
  step "构建 Aegis 镜像（首次构建需下载依赖，请耐心等待）..."
  compose --profile app build "${BUILD_ARGS[@]}" server
  step "启动全栈（基础设施 → 自动迁移 → 后端）..."
  compose --profile app --profile ui up -d
  step "等待服务健康检查通过..."
  for i in $(seq 1 60); do
    state="$(docker inspect -f '{{.State.Health.Status}}' aegis-server 2>/dev/null || echo starting)"
    [ "$state" = "healthy" ] && break
    [ "$state" = "unhealthy" ] && { docker logs --tail 30 aegis-server; die "aegis-server 健康检查失败，完整日志：docker logs aegis-server"; }
    sleep 2
  done
  [ "${state:-}" = "healthy" ] || warn "等待超时，服务可能仍在启动中：docker logs -f aegis-server"
fi

# ── 部署信息汇总 ──
get_env() { grep -E "^$1=" "$ENV_FILE" | head -1 | cut -d= -f2-; }
HTTP_PORT="$(get_env HTTP_PORT)"; HTTP_PORT="${HTTP_PORT:-8088}"

echo ""
echo -e "${BOLD}══════════════ Aegis 部署完成 ══════════════${NC}"
if [ "$MODE" = "app" ]; then
  echo -e "  后端 API      ${GREEN}http://localhost:${HTTP_PORT}${NC}"
  echo -e "  健康检查      http://localhost:${HTTP_PORT}/healthz"
  echo -e "  API 文档      http://localhost:${HTTP_PORT}/docs"
  echo -e "  Temporal UI   http://localhost:$(get_env TEMPORAL_UI_PORT || echo 8233)"
  echo -e "  NATS UI       http://localhost:$(get_env NATS_UI_PORT || echo 31311)"
  echo ""
  echo -e "  超管账号      $(get_env ADMIN_BOOTSTRAP_ACCOUNT)"
  echo -e "  超管密码      $(get_env ADMIN_BOOTSTRAP_PASSWORD)"
  echo -e "  Admin Token   $(get_env ADMIN_API_TOKEN)"
  echo ""
  echo -e "  管理前端      cd aegis-console && pnpm install && pnpm dev"
else
  echo -e "  PostgreSQL    localhost:$(get_env AEGIS_DB_PORT || echo 15432)"
  echo -e "  Redis         localhost:6379    NATS  localhost:4222"
fi
echo -e "  常用命令      $0 --status / --down"
echo -e "${BOLD}═════════════════════════════════════════════${NC}"
