#!/usr/bin/env bash
# 开发环境后台启动/停止：后端 go run（:8080）+ 前端 vite dev（:5173）。
# PID 与日志记录在 .run/ 下，供 stop 与排查使用。
set -uo pipefail
cd "$(dirname "$0")/.."
mkdir -p .run

is_running() {
  local pidfile=".run/$1.pid"
  [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null
}

start_service() {
  local name="$1"
  shift
  if is_running "$name"; then
    echo "$name 已在运行 (pid $(cat ".run/$name.pid"))"
    return
  fi
  nohup "$@" >".run/$name.log" 2>&1 </dev/null &
  local pid=$!
  echo "$pid" >".run/$name.pid"
  echo "已启动 $name (pid $pid): $*"
}

stop_service() {
  local name="$1"
  local pidfile=".run/$name.pid"
  if ! is_running "$name"; then
    echo "$name 未在运行"
    rm -f "$pidfile"
    return
  fi
  local pid
  pid=$(cat "$pidfile")
  # go run / npm 都会派生子进程：先杀孙，再杀子，最后杀本体
  for child in $(pgrep -P "$pid" 2>/dev/null); do
    pkill -P "$child" 2>/dev/null
  done
  pkill -P "$pid" 2>/dev/null
  kill "$pid" 2>/dev/null
  echo "已停止 $name (pid $pid)"
  rm -f "$pidfile"
}

case "${1:-}" in
start)
  [ -d web/node_modules ] || npm --prefix web ci
  [ -d internal/web/dist ] || npm --prefix web run build
  start_service backend go run ./cmd/note
  start_service frontend npm --prefix web run dev
  echo "后端 http://127.0.0.1:8080 · 前端 http://localhost:5173 （/api 已代理到后端）"
  echo "日志：.run/backend.log .run/frontend.log"
  ;;
stop)
  stop_service frontend
  stop_service backend
  ;;
*)
  echo "用法: $0 {start|stop}" >&2
  exit 1
  ;;
esac
