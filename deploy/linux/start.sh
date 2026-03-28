#!/usr/bin/env bash
# Aegis 启动脚本（带看门狗自动重启）
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNTIME_DIR="$ROOT_DIR/.runtime"
SERVER_BIN="$RUNTIME_DIR/bin/aegis-server"
PID_FILE="$RUNTIME_DIR/run/server.pid"
STOP_FILE="$RUNTIME_DIR/run/server.stop"
STDOUT_LOG="$RUNTIME_DIR/logs/server.stdout.log"
STDERR_LOG="$RUNTIME_DIR/logs/server.stderr.log"
WATCHDOG_LOG="$RUNTIME_DIR/logs/server.watchdog.log"

MAX_RESTARTS=10
RESTART_WINDOW=300
INITIAL_BACKOFF=1
MAX_BACKOFF=30
STABLE_SECONDS=60

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

step() { echo -e "${CYAN}[AEGIS]${NC} $1"; }
wlog() {
  local msg="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
  echo "$msg" >> "$WATCHDOG_LOG"
  echo -e "${CYAN}[AEGIS]${NC} $1"
}

mkdir -p "$RUNTIME_DIR"/{bin,logs,run}

# 检查是否已运行
if [ -f "$PID_FILE" ]; then
  existing_pid=$(cat "$PID_FILE" 2>/dev/null | tr -d '[:space:]')
  if [ -n "$existing_pid" ] && kill -0 "$existing_pid" 2>/dev/null; then
    step "Server already running, pid=$existing_pid"
    exit 0
  fi
  rm -f "$PID_FILE"
fi

if [ ! -x "$SERVER_BIN" ]; then
  echo -e "\033[0;31m[AEGIS] Server binary not found: $SERVER_BIN. Run deploy.sh first.\033[0m"
  exit 1
fi

# 清除旧的停止信号
rm -f "$STOP_FILE"

restart_count=0
window_start=$SECONDS
backoff=$INITIAL_BACKOFF

step "Starting server with watchdog (auto-restart on crash)"

while true; do
  # 重置重启窗口
  elapsed=$(( SECONDS - window_start ))
  if [ $elapsed -gt $RESTART_WINDOW ]; then
    restart_count=0
    window_start=$SECONDS
    backoff=$INITIAL_BACKOFF
  fi

  # 检查重启限制
  if [ $restart_count -ge $MAX_RESTARTS ]; then
    wlog "ABORT: Restarted $restart_count times in ${RESTART_WINDOW}s, giving up"
    rm -f "$PID_FILE"
    exit 1
  fi

  # 启动进程
  cd "$ROOT_DIR"
  "$SERVER_BIN" >> "$STDOUT_LOG" 2>> "$STDERR_LOG" &
  server_pid=$!
  echo "$server_pid" > "$PID_FILE"

  if [ $restart_count -eq 0 ]; then
    wlog "Started server, pid=$server_pid"
  else
    wlog "Restarted server, pid=$server_pid, attempt=$restart_count"
  fi

  process_start=$SECONDS
  wait "$server_pid" 2>/dev/null
  exit_code=$?
  run_duration=$(( SECONDS - process_start ))

  # 检查停止信号
  if [ -f "$STOP_FILE" ]; then
    wlog "Stopped by signal (code=$exit_code), not restarting"
    rm -f "$PID_FILE" "$STOP_FILE"
    exit 0
  fi

  # 正常退出
  if [ $exit_code -eq 0 ]; then
    wlog "Exited normally (code=0), not restarting"
    rm -f "$PID_FILE"
    exit 0
  fi

  wlog "Crashed (code=$exit_code, ran ${run_duration}s)"

  # 稳定运行后重置退避
  if [ $run_duration -ge $STABLE_SECONDS ]; then
    backoff=$INITIAL_BACKOFF
  fi

  restart_count=$(( restart_count + 1 ))

  wlog "Waiting ${backoff}s before restart..."
  sleep "$backoff"
  backoff=$(( backoff * 2 ))
  if [ $backoff -gt $MAX_BACKOFF ]; then
    backoff=$MAX_BACKOFF
  fi
done
