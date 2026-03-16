#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${APP_HOME:-$(cd "${SCRIPT_DIR}/.." && pwd)}"
BIN_DIR="${ROOT_DIR}/bin"
RUN_DIR="${ROOT_DIR}/run"
LOG_DIR="${ROOT_DIR}/logs"
TMP_DIR="${ROOT_DIR}/tmp"

APP_NAME="wakego"
BIN_PATH="${BIN_DIR}/${APP_NAME}"
PID_FILE="${RUN_DIR}/${APP_NAME}.pid"
CONFIG_FILE="${ROOT_DIR}/config.json"
LOG_FILE="${LOG_DIR}/${APP_NAME}.log"
ADDR="${ADDR:-:9090}"
VERSION="${VERSION:-latest}"
REPO="${REPO:-${GITHUB_REPO:-}}"
COMMAND="${1:-start}"

mkdir -p "${BIN_DIR}" "${RUN_DIR}" "${LOG_DIR}" "${TMP_DIR}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[deploy] missing required command: $1" >&2
    exit 1
  fi
}

resolve_repo() {
  if [[ -n "${REPO}" ]]; then
    echo "${REPO}"
    return 0
  fi

  if command -v git >/dev/null 2>&1 && git -C "${ROOT_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    local remote
    remote="$(git -C "${ROOT_DIR}" config --get remote.origin.url || true)"
    if [[ "${remote}" =~ github\.com[:/]([^/]+/[^/.]+)(\.git)?$ ]]; then
      echo "${BASH_REMATCH[1]}"
      return 0
    fi
  fi

  echo "[deploy] set REPO=owner/repo or configure git remote.origin.url" >&2
  exit 1
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      echo "[deploy] unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "[deploy] unsupported ARCH: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

asset_name() {
  local os arch
  os="$(detect_os)"
  arch="$(detect_arch)"
  echo "${APP_NAME}_${os}_${arch}.tar.gz"
}

download_url() {
  local repo asset
  repo="$(resolve_repo)"
  asset="$(asset_name)"

  if [[ "${VERSION}" == "latest" ]]; then
    echo "https://github.com/${repo}/releases/latest/download/${asset}"
  else
    echo "https://github.com/${repo}/releases/download/${VERSION}/${asset}"
  fi
}

install() {
  require_cmd curl
  require_cmd tar

  local archive url
  archive="${TMP_DIR}/$(asset_name)"
  url="$(download_url)"

  echo "[deploy] downloading ${url}"
  curl -fL --retry 3 --connect-timeout 10 -o "${archive}" "${url}"

  mkdir -p "${BIN_DIR}"
  tar -xzf "${archive}" -C "${BIN_DIR}"
  chmod +x "${BIN_PATH}"
  echo "[deploy] installed ${BIN_PATH}"
}

ensure_config() {
  if [[ ! -f "${CONFIG_FILE}" ]]; then
    if [[ -f "${ROOT_DIR}/config.example.json" ]]; then
      cp "${ROOT_DIR}/config.example.json" "${CONFIG_FILE}"
    else
      cat > "${CONFIG_FILE}" <<'EOF'
{
  "title": "WOL 控制台",
  "admin_password": "123456",
  "default_port": 9,
  "devices": []
}
EOF
    fi
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

  install
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

update() {
  local was_running=0
  if is_running; then
    was_running=1
    stop
  fi

  install

  if [[ "${was_running}" -eq 1 ]]; then
    start
  fi
}

case "${COMMAND}" in
  install)
    install
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
  update)
    update
    ;;
  status)
    status
    ;;
  logs)
    logs
    ;;
  *)
    echo "usage: $0 {install|start|stop|restart|update|status|logs}" >&2
    exit 1
    ;;
esac
