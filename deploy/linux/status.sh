#!/usr/bin/env bash
# Aegis 状态检查脚本
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNTIME_DIR="$ROOT_DIR/.runtime"
PID_FILE="$RUNTIME_DIR/run/server.pid"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}●${NC} $1"; }
fail() { echo -e "  ${RED}●${NC} $1"; }
info() { echo -e "  ${YELLOW}○${NC} $1"; }

echo -e "${CYAN}═══ Aegis Runtime Status ═══${NC}"
echo ""

# ── 服务进程 ──
echo -e "${CYAN}Server Process${NC}"
if [ -f "$PID_FILE" ]; then
  server_pid=$(cat "$PID_FILE" 2>/dev/null | tr -d '[:space:]')
  if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
    uptime_info=""
    if [ -d "/proc/$server_pid" ]; then
      start_time=$(stat -c %Y "/proc/$server_pid" 2>/dev/null || echo "")
      if [ -n "$start_time" ]; then
        now=$(date +%s)
        elapsed=$(( now - start_time ))
        hours=$(( elapsed / 3600 ))
        mins=$(( (elapsed % 3600) / 60 ))
        uptime_info=" (uptime: ${hours}h${mins}m)"
      fi
    fi
    ok "Running, pid=$server_pid$uptime_info"
  else
    fail "Not running (stale PID: $server_pid)"
  fi
else
  fail "Not running (no PID file)"
fi
echo ""

# ── Docker 容器 ──
echo -e "${CYAN}Docker Containers${NC}"
containers=("aegis-postgres" "aegis-redis" "aegis-nats" "aegis-temporal-postgres" "aegis-temporal" "aegis-rdkit-captcha")
for name in "${containers[@]}"; do
  status=$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo "not found")
  health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}n/a{{end}}' "$name" 2>/dev/null || echo "")
  label="$name"
  if [ "$status" = "running" ]; then
    if [ "$health" = "healthy" ]; then
      ok "$label  [running, healthy]"
    elif [ "$health" = "unhealthy" ]; then
      fail "$label  [running, unhealthy]"
    else
      ok "$label  [running]"
    fi
  elif [ "$status" = "not found" ]; then
    fail "$label  [not found]"
  else
    fail "$label  [$status]"
  fi
done
echo ""

# ── 端口检查 ──
echo -e "${CYAN}Port Connectivity${NC}"
check_port() {
  local port="$1" name="$2"
  if timeout 1 bash -c "echo >/dev/tcp/127.0.0.1/$port" 2>/dev/null; then
    ok "$name  :$port"
  else
    fail "$name  :$port"
  fi
}

check_port 5432 "PostgreSQL"
check_port 6379 "Redis"
check_port 4222 "NATS"
check_port 7233 "Temporal"
check_port 5050 "RDKit Captcha"
check_port 8088 "Aegis Server"
echo ""

# ── 日志文件 ──
echo -e "${CYAN}Log Files${NC}"
for logfile in "$RUNTIME_DIR/logs"/*.log; do
  if [ -f "$logfile" ]; then
    size=$(du -h "$logfile" | cut -f1)
    info "$(basename "$logfile")  ($size)"
  fi
done
echo ""
