#!/usr/bin/env bash
# Aegis 一键部署脚本 (Linux)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/deploy/docker/docker-compose.yml"
RUNTIME_DIR="$ROOT_DIR/.runtime"
SERVER_BIN="$RUNTIME_DIR/bin/aegis-server"
ENV_FILE="$ROOT_DIR/.env"
ENV_TEMPLATE="$SCRIPT_DIR/.env.linux.example"
SKIP_TESTS=false
FORCE_ENV=false
NO_START=false

# ── 参数解析 ──
for arg in "$@"; do
  case "$arg" in
    --skip-tests) SKIP_TESTS=true ;;
    --force-env)  FORCE_ENV=true ;;
    --no-start)   NO_START=true ;;
  esac
done

# ── 颜色输出 ──
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

step()  { echo -e "${CYAN}[AEGIS]${NC} $1"; }
ok()    { echo -e "${GREEN}[AEGIS]${NC} $1"; }
warn()  { echo -e "${YELLOW}[AEGIS]${NC} $1"; }
fail()  { echo -e "${RED}[AEGIS]${NC} $1"; exit 1; }

# ── 依赖检查 ──
step "Checking dependencies..."

command -v go >/dev/null 2>&1 || fail "Go not found. Please install Go 1.24+ and add it to PATH."
GO_VER=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
ok "Go $GO_VER"

command -v docker >/dev/null 2>&1 || fail "Docker not found. Please install Docker."
if docker compose version >/dev/null 2>&1; then
  COMPOSE_CMD="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE_CMD="docker-compose"
else
  fail "docker compose not found."
fi
ok "Docker Compose ready"

# ── 运行时目录 ──
step "Preparing runtime layout..."
mkdir -p "$RUNTIME_DIR"/{bin,logs,run}

# ── 环境变量 ──
if [ "$FORCE_ENV" = true ] || [ ! -f "$ENV_FILE" ]; then
  cp "$ENV_TEMPLATE" "$ENV_FILE"
  ok "Environment file ready: $ENV_FILE"
else
  ok "Environment file exists: $ENV_FILE"
fi

# ── PostgreSQL 版本检查 + 自动迁移 ──
COMPOSE_PROJECT=$(basename "$(dirname "$COMPOSE_FILE")")
PG_VOLUME="${COMPOSE_PROJECT}_postgres_data"
PG_DUMP_FILE=""
TARGET_PG_MAJOR=17

step "Checking PostgreSQL version compatibility..."

pg_upgrade() {
  local vol_name="$1" db_user="$2" label="$3"

  # 检查卷是否存在
  if ! docker volume ls -q --filter "name=${vol_name}" | grep -qx "${vol_name}"; then
    ok "$label: Volume not found, fresh install"
    return
  fi

  # 读取 PG_VERSION
  local current_ver
  current_ver=$(docker run --rm -v "${vol_name}:/pgdata" alpine cat /pgdata/PG_VERSION 2>/dev/null || echo "")
  if [ -z "$current_ver" ]; then
    ok "$label: Cannot read PG_VERSION, skipping"
    return
  fi
  current_ver=$(echo "$current_ver" | tr -d '[:space:]')

  if [ "$current_ver" -ge "$TARGET_PG_MAJOR" ] 2>/dev/null; then
    ok "$label: PG $current_ver >= $TARGET_PG_MAJOR, no upgrade needed"
    return
  fi

  step "$label: Upgrade PG $current_ver -> $TARGET_PG_MAJOR"

  # 停止占用卷的容器
  docker rm -f "aegis-${label}" 2>/dev/null || true

  local old_image="postgres:${current_ver}-alpine"
  local temp_container="aegis-pgupgrade-${label}"
  PG_DUMP_FILE=$(mktemp "/tmp/aegis_pgupgrade_${label}.XXXXXX.sql")

  docker rm -f "$temp_container" 2>/dev/null || true

  step "$label: Starting temp PG $current_ver container..."
  docker run -d --name "$temp_container" \
    -v "${vol_name}:/var/lib/postgresql/data" \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    "$old_image" >/dev/null

  step "$label: Waiting for PG ready..."
  local deadline=$((SECONDS + 60))
  while [ $SECONDS -lt $deadline ]; do
    if docker exec "$temp_container" pg_isready -U "$db_user" >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done

  step "$label: Running pg_dumpall..."
  docker exec "$temp_container" bash -c "pg_dumpall -U ${db_user} > /tmp/pgdump.sql"
  docker cp "${temp_container}:/tmp/pgdump.sql" "$PG_DUMP_FILE"
  local dump_size
  dump_size=$(du -h "$PG_DUMP_FILE" | cut -f1)
  ok "$label: Dump complete ($dump_size)"

  docker rm -f "$temp_container" >/dev/null 2>&1 || true

  step "$label: Removing old volume ${vol_name}..."
  docker volume rm "$vol_name" >/dev/null 2>&1 || true
}

pg_upgrade "$PG_VOLUME" "aegis" "postgres"

# Temporal: 仅在主库升级时才重建
TEMPORAL_VOLUME="${COMPOSE_PROJECT}_temporal_postgres_data"
if [ -n "$PG_DUMP_FILE" ] && [ -f "$PG_DUMP_FILE" ]; then
  step "temporal-postgres: Removing old volume (auto-setup will recreate)..."
  docker rm -f aegis-temporal aegis-temporal-postgres 2>/dev/null || true
  docker volume rm "$TEMPORAL_VOLUME" 2>/dev/null || true
fi

# ── 启动 Docker 依赖 ──
step "Starting Docker dependencies..."
$COMPOSE_CMD -f "$COMPOSE_FILE" up -d postgres redis nats temporal rdkit-captcha

# ── 等待端口就绪 ──
wait_port() {
  local host="$1" port="$2" name="$3" timeout="${4:-90}"
  local deadline=$((SECONDS + timeout))
  while [ $SECONDS -lt $deadline ]; do
    if timeout 1 bash -c "echo >/dev/tcp/$host/$port" 2>/dev/null; then
      ok "Dependency ready: $name"
      return
    fi
    sleep 2
  done
  fail "Timed out waiting for $name ($host:$port)"
}

wait_port 127.0.0.1 5432 "PostgreSQL"
wait_port 127.0.0.1 6379 "Redis"
wait_port 127.0.0.1 4222 "NATS"
wait_port 127.0.0.1 7233 "Temporal"
wait_port 127.0.0.1 5050 "RDKit Captcha"

# ── 恢复数据库备份 ──
if [ -n "$PG_DUMP_FILE" ] && [ -f "$PG_DUMP_FILE" ] && [ -s "$PG_DUMP_FILE" ]; then
  step "Restoring database dump..."
  docker cp "$PG_DUMP_FILE" aegis-postgres:/tmp/pgupgrade_restore.sql
  docker exec aegis-postgres psql -U aegis -d postgres -f /tmp/pgupgrade_restore.sql >/dev/null 2>&1 || true
  docker exec aegis-postgres rm -f /tmp/pgupgrade_restore.sql
  rm -f "$PG_DUMP_FILE"
  ok "Database restore complete"
fi

# ── 等待 PG 完全就绪 ──
step "Waiting for PostgreSQL ready (pg_isready)..."
deadline=$((SECONDS + 60))
while [ $SECONDS -lt $deadline ]; do
  if docker exec aegis-postgres pg_isready -U aegis >/dev/null 2>&1; then
    ok "PostgreSQL ready"
    break
  fi
  sleep 2
done

# ── 等待 Temporal ready ──
step "Waiting for Temporal auto-setup..."
deadline=$((SECONDS + 120))
while [ $SECONDS -lt $deadline ]; do
  TEMPORAL_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' aegis-temporal 2>/dev/null || echo "")
  if [ -n "$TEMPORAL_IP" ]; then
    if docker exec aegis-temporal temporal operator namespace describe -n default --address "${TEMPORAL_IP}:7233" >/dev/null 2>&1; then
      ok "Temporal ready"
      break
    fi
  fi
  sleep 3
done

# ── Go 编译 ──
step "Building unified server binary..."
cd "$ROOT_DIR"
go build -o "$SERVER_BIN" ./cmd/server
ok "Build complete: $SERVER_BIN"

# ── 数据库迁移 ──
step "Running PostgreSQL migrations..."
"$SERVER_BIN" migrate
ok "Migrations complete"

# ── 测试 ──
if [ "$SKIP_TESTS" = false ]; then
  step "Running Go test suite..."
  go test ./... || fail "Tests failed"
  ok "Tests passed"
fi

# ── 启动 ──
if [ "$NO_START" = false ]; then
  step "Starting Aegis runtime..."
  exec "$SCRIPT_DIR/start.sh"
else
  ok "Deployment finished without starting runtime"
fi
