#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
RUN_DIR="${ROOT_DIR}/run"
LOG_DIR="${ROOT_DIR}/logs"
GOCACHE_DIR="${ROOT_DIR}/.gocache"

APP_NAME="wakego"
BIN_PATH="${BIN_DIR}/${APP_NAME}"
PID_FILE="${RUN_DIR}/${APP_NAME}.pid"
CONFIG_FILE="${ROOT_DIR}/config.json"
LOG_FILE="${LOG_DIR}/${APP_NAME}.log"
ADDR="${ADDR:-:9090}"
COMMAND="${1:-start}"

mkdir -p "${BIN_DIR}" "${RUN_DIR}" "${LOG_DIR}" "${GOCACHE_DIR}"

build() {
  echo "[deploy] building ${APP_NAME}"
  env GOCACHE="${GOCACHE_DIR}" go build -o "${BIN_PATH}" ./cmd/wakego
}

ensure_config() {
  if [[ ! -f "${CONFIG_FILE}" ]]; then
    cp "${ROOT_DIR}/config.example.json" "${CONFIG_FILE}"
    echo "[deploy] created default config at ${CONFIG_FILE}"
  fi
}

is_running() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid="$(cat "${PID_FILE}")"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      return 0
    fi
  fi
  return 1
}

start() {
  if is_running; then
    echo "[deploy] ${APP_NAME} is already running with pid $(cat "${PID_FILE}")"
    return 0
  fi

  build
  ensure_config

  echo "[deploy] starting ${APP_NAME} on ${ADDR}"
  nohup "${BIN_PATH}" -addr "${ADDR}" -config "${CONFIG_FILE}" -log-file "${LOG_FILE}" >/dev/null 2>&1 &
  echo $! > "${PID_FILE}"
  sleep 1

  if is_running; then
    echo "[deploy] started pid $(cat "${PID_FILE}")"
    echo "[deploy] open http://127.0.0.1:${ADDR#:}"
  else
    echo "[deploy] start failed, check ${LOG_FILE}" >&2
    exit 1
  fi
}

stop() {
  if ! is_running; then
    echo "[deploy] ${APP_NAME} is not running"
    rm -f "${PID_FILE}"
    return 0
  fi

  local pid
  pid="$(cat "${PID_FILE}")"
  echo "[deploy] stopping pid ${pid}"
  kill "${pid}"
  rm -f "${PID_FILE}"
}

status() {
  if is_running; then
    echo "[deploy] ${APP_NAME} is running with pid $(cat "${PID_FILE}") on ${ADDR}"
  else
    echo "[deploy] ${APP_NAME} is not running"
  fi
}

logs() {
  touch "${LOG_FILE}"
  tail -n 200 -f "${LOG_FILE}"
}

restart() {
  stop || true
  start
}

case "${COMMAND}" in
  build)
    build
    ;;
  start)
    start
    ;;
  stop)
    stop
    ;;
  restart)
    restart
    ;;
  status)
    status
    ;;
  logs)
    logs
    ;;
  *)
    echo "usage: $0 {build|start|stop|restart|status|logs}" >&2
    exit 1
    ;;
esac
