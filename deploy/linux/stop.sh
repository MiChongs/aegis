#!/usr/bin/env bash
# Aegis 优雅停止脚本
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNTIME_DIR="$ROOT_DIR/.runtime"
PID_FILE="$RUNTIME_DIR/run/server.pid"
STOP_FILE="$RUNTIME_DIR/run/server.stop"
GRACEFUL_TIMEOUT=${1:-10}

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

step() { echo -e "${CYAN}[AEGIS]${NC} $1"; }

if [ ! -f "$PID_FILE" ]; then
  step "Server is not running (no PID file)"
  rm -f "$STOP_FILE"
  exit 0
fi

server_pid=$(cat "$PID_FILE" 2>/dev/null | tr -d '[:space:]')
if [ -z "$server_pid" ] || ! kill -0 "$server_pid" 2>/dev/null; then
  step "Server is not running (stale PID file)"
  rm -f "$PID_FILE" "$STOP_FILE"
  exit 0
fi

step "Stopping server, pid=$server_pid..."

# 1) 创建停止信号文件（Go 进程 watchStopFile 检测）
echo "stop" > "$STOP_FILE"

# 2) 同时发送 SIGTERM（Go signal.NotifyContext 捕获）
kill -TERM "$server_pid" 2>/dev/null || true

# 3) 等待优雅退出
deadline=$(( SECONDS + GRACEFUL_TIMEOUT ))
exited=false
while [ $SECONDS -lt $deadline ]; do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    exited=true
    break
  fi
  sleep 0.3
done

# 4) 超时强杀
if [ "$exited" = false ]; then
  step "Did not exit within ${GRACEFUL_TIMEOUT}s, force killing..."
  kill -9 "$server_pid" 2>/dev/null || true
  sleep 0.3
fi

rm -f "$PID_FILE" "$STOP_FILE"
echo -e "${GREEN}[AEGIS]${NC} Stopped server, pid=$server_pid"
