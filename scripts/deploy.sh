#!/usr/bin/env bash
set -euo pipefail

DEFAULT_APP_HOME="${HOME}/wakego"
SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
resolve_root_dir() {
  if [[ -n "${APP_HOME:-}" ]]; then
    echo "${APP_HOME}"
    return 0
  fi

  if [[ -n "${SCRIPT_SOURCE}" && "${SCRIPT_SOURCE}" != "bash" && -e "${SCRIPT_SOURCE}" ]]; then
    local script_dir candidate
    script_dir="$(cd "$(dirname "${SCRIPT_SOURCE}")" && pwd)"
    candidate="$(cd "${script_dir}/.." && pwd)"
    if [[ -f "${candidate}/config.example.json" || -d "${candidate}/.git" ]]; then
      echo "${candidate}"
      return 0
    fi
  fi

  echo "${DEFAULT_APP_HOME}"
}

ROOT_DIR="$(resolve_root_dir)"
BIN_DIR="${ROOT_DIR}/bin"
RUN_DIR="${ROOT_DIR}/run"
LOG_DIR="${ROOT_DIR}/logs"
TMP_DIR="${ROOT_DIR}/tmp"

APP_NAME="wakego"
SERVICE_LABEL="com.wakego.wakego"
SYSTEMD_SERVICE_NAME="wakego.service"
BIN_PATH="${BIN_DIR}/${APP_NAME}"
PID_FILE="${RUN_DIR}/${APP_NAME}.pid"
CONFIG_FILE="${ROOT_DIR}/config.json"
LOG_FILE="${LOG_DIR}/${APP_NAME}.log"
ADDR="${ADDR:-:9090}"
VERSION="${VERSION:-latest}"
REPO="${REPO:-${GITHUB_REPO:-gofxq/wakeonlango}}"
COMMAND="${1:-start}"

mkdir -p "${BIN_DIR}" "${RUN_DIR}" "${LOG_DIR}" "${TMP_DIR}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[deploy] missing required command: $1" >&2
    exit 1
  fi
}

write_systemd_unit() {
  local service_file="$1"
  local wanted_by="$2"
  mkdir -p "$(dirname "${service_file}")"
  cat > "${service_file}" <<EOF
[Unit]
Description=WakeGo Wake-on-LAN Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${ROOT_DIR}
ExecStart=${BIN_PATH} -addr ${ADDR} -config ${CONFIG_FILE} -log-file ${LOG_FILE}
Restart=always
RestartSec=3

[Install]
WantedBy=${wanted_by}
EOF
}

write_launchd_plist() {
  local plist_file="$1"
  mkdir -p "$(dirname "${plist_file}")"
  cat > "${plist_file}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${SERVICE_LABEL}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${BIN_PATH}</string>
    <string>-addr</string>
    <string>${ADDR}</string>
    <string>-config</string>
    <string>${CONFIG_FILE}</string>
    <string>-log-file</string>
    <string>${LOG_FILE}</string>
  </array>
  <key>WorkingDirectory</key>
  <string>${ROOT_DIR}</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
EOF
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

service_mode() {
  if [[ -f "/etc/systemd/system/${SYSTEMD_SERVICE_NAME}" ]]; then
    echo "systemd-system"
    return 0
  fi
  if [[ -f "${HOME}/.config/systemd/user/${SYSTEMD_SERVICE_NAME}" ]]; then
    echo "systemd-user"
    return 0
  fi
  if [[ -f "/Library/LaunchDaemons/${SERVICE_LABEL}.plist" ]]; then
    echo "launchd-system"
    return 0
  fi
  if [[ -f "${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist" ]]; then
    echo "launchd-user"
    return 0
  fi
  return 1
}

start_managed_service() {
  case "$(service_mode)" in
    systemd-system)
      systemctl start "${SYSTEMD_SERVICE_NAME}"
      ;;
    systemd-user)
      systemctl --user start "${SYSTEMD_SERVICE_NAME}"
      ;;
    launchd-system)
      launchctl bootout "system" "/Library/LaunchDaemons/${SERVICE_LABEL}.plist" >/dev/null 2>&1 || true
      launchctl bootstrap "system" "/Library/LaunchDaemons/${SERVICE_LABEL}.plist"
      launchctl enable "system/${SERVICE_LABEL}" >/dev/null 2>&1 || true
      ;;
    launchd-user)
      launchctl bootout "gui/$(id -u)" "${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist" >/dev/null 2>&1 || true
      launchctl bootstrap "gui/$(id -u)" "${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist"
      launchctl enable "gui/$(id -u)/${SERVICE_LABEL}" >/dev/null 2>&1 || true
      ;;
  esac
}

stop_managed_service() {
  case "$(service_mode)" in
    systemd-system)
      systemctl stop "${SYSTEMD_SERVICE_NAME}"
      ;;
    systemd-user)
      systemctl --user stop "${SYSTEMD_SERVICE_NAME}"
      ;;
    launchd-system)
      launchctl bootout "system" "/Library/LaunchDaemons/${SERVICE_LABEL}.plist"
      ;;
    launchd-user)
      launchctl bootout "gui/$(id -u)" "${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist"
      ;;
  esac
}

restart_managed_service() {
  case "$(service_mode)" in
    systemd-system)
      systemctl restart "${SYSTEMD_SERVICE_NAME}"
      ;;
    systemd-user)
      systemctl --user restart "${SYSTEMD_SERVICE_NAME}"
      ;;
    launchd-system)
      launchctl bootout "system" "/Library/LaunchDaemons/${SERVICE_LABEL}.plist" >/dev/null 2>&1 || true
      launchctl bootstrap "system" "/Library/LaunchDaemons/${SERVICE_LABEL}.plist"
      launchctl enable "system/${SERVICE_LABEL}" >/dev/null 2>&1 || true
      ;;
    launchd-user)
      launchctl bootout "gui/$(id -u)" "${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist" >/dev/null 2>&1 || true
      launchctl bootstrap "gui/$(id -u)" "${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist"
      launchctl enable "gui/$(id -u)/${SERVICE_LABEL}" >/dev/null 2>&1 || true
      ;;
  esac
}

status_managed_service() {
  case "$(service_mode)" in
    systemd-system)
      systemctl status "${SYSTEMD_SERVICE_NAME}" --no-pager
      ;;
    systemd-user)
      systemctl --user status "${SYSTEMD_SERVICE_NAME}" --no-pager
      ;;
    launchd-system)
      launchctl print "system/${SERVICE_LABEL}"
      ;;
    launchd-user)
      launchctl print "gui/$(id -u)/${SERVICE_LABEL}"
      ;;
  esac
}

start() {
  if service_mode >/dev/null 2>&1; then
    start_managed_service
    echo "[deploy] started managed service $(service_mode)"
    return 0
  fi

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
  if service_mode >/dev/null 2>&1; then
    stop_managed_service
    echo "[deploy] stopped managed service $(service_mode)"
    return 0
  fi

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
  if service_mode >/dev/null 2>&1; then
    status_managed_service
    return 0
  fi

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
  if service_mode >/dev/null 2>&1; then
    restart_managed_service
    echo "[deploy] restarted managed service $(service_mode)"
    return 0
  fi

  stop || true
  start
}

update() {
  local mode was_running=0
  mode="$(service_mode || true)"

  if [[ -n "${mode}" ]]; then
    install
    restart_managed_service
    echo "[deploy] updated managed service ${mode}"
    return 0
  fi

  if is_running; then
    was_running=1
    stop
  fi

  install

  if [[ "${was_running}" -eq 1 ]]; then
    start
  fi
}

enable_systemd() {
  require_cmd systemctl

  local service_file
  if [[ "${EUID}" -eq 0 ]]; then
    service_file="/etc/systemd/system/${SYSTEMD_SERVICE_NAME}"
    write_systemd_unit "${service_file}" "multi-user.target"
    systemctl daemon-reload
    systemctl enable --now "${SYSTEMD_SERVICE_NAME}"
    echo "[deploy] enabled systemd service ${SYSTEMD_SERVICE_NAME}"
    return 0
  fi

  service_file="${HOME}/.config/systemd/user/${SYSTEMD_SERVICE_NAME}"
  write_systemd_unit "${service_file}" "default.target"
  systemctl --user daemon-reload
  systemctl --user enable --now "${SYSTEMD_SERVICE_NAME}"
  echo "[deploy] enabled user systemd service ${SYSTEMD_SERVICE_NAME}"
  if command -v loginctl >/dev/null 2>&1; then
    if ! loginctl show-user "${USER}" -p Linger 2>/dev/null | grep -q "Linger=yes"; then
      echo "[deploy] note: run 'sudo loginctl enable-linger ${USER}' if you want reboot auto-start before login"
    fi
  fi
}

disable_systemd() {
  require_cmd systemctl

  if [[ "${EUID}" -eq 0 ]]; then
    systemctl disable --now "${SYSTEMD_SERVICE_NAME}" 2>/dev/null || true
    rm -f "/etc/systemd/system/${SYSTEMD_SERVICE_NAME}"
    systemctl daemon-reload
    echo "[deploy] disabled systemd service ${SYSTEMD_SERVICE_NAME}"
    return 0
  fi

  systemctl --user disable --now "${SYSTEMD_SERVICE_NAME}" 2>/dev/null || true
  rm -f "${HOME}/.config/systemd/user/${SYSTEMD_SERVICE_NAME}"
  systemctl --user daemon-reload
  echo "[deploy] disabled user systemd service ${SYSTEMD_SERVICE_NAME}"
}

enable_launchd() {
  require_cmd launchctl

  local domain plist_file
  if [[ "${EUID}" -eq 0 ]]; then
    domain="system"
    plist_file="/Library/LaunchDaemons/${SERVICE_LABEL}.plist"
  else
    domain="gui/$(id -u)"
    plist_file="${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist"
  fi

  write_launchd_plist "${plist_file}"
  launchctl bootout "${domain}" "${plist_file}" >/dev/null 2>&1 || true
  launchctl bootstrap "${domain}" "${plist_file}"
  launchctl enable "${domain}/${SERVICE_LABEL}" >/dev/null 2>&1 || true
  echo "[deploy] enabled launchd job ${SERVICE_LABEL}"
  if [[ "${EUID}" -ne 0 ]]; then
    echo "[deploy] note: user LaunchAgent starts after login; use sudo for system-wide boot startup"
  fi
}

disable_launchd() {
  require_cmd launchctl

  local domain plist_file
  if [[ "${EUID}" -eq 0 ]]; then
    domain="system"
    plist_file="/Library/LaunchDaemons/${SERVICE_LABEL}.plist"
  else
    domain="gui/$(id -u)"
    plist_file="${HOME}/Library/LaunchAgents/${SERVICE_LABEL}.plist"
  fi

  launchctl bootout "${domain}" "${plist_file}" >/dev/null 2>&1 || true
  rm -f "${plist_file}"
  echo "[deploy] disabled launchd job ${SERVICE_LABEL}"
}

enable() {
  install
  ensure_config
  stop || true

  case "$(uname -s)" in
    Linux)
      enable_systemd
      ;;
    Darwin)
      enable_launchd
      ;;
    *)
      echo "[deploy] enable is not supported on $(uname -s)" >&2
      exit 1
      ;;
  esac
}

disable() {
  case "$(uname -s)" in
    Linux)
      disable_systemd
      ;;
    Darwin)
      disable_launchd
      ;;
    *)
      echo "[deploy] disable is not supported on $(uname -s)" >&2
      exit 1
      ;;
  esac
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
  enable)
    enable
    ;;
  disable)
    disable
    ;;
  status)
    status
    ;;
  logs)
    logs
    ;;
  *)
    echo "usage: $0 {install|start|stop|restart|update|enable|disable|status|logs}" >&2
    exit 1
    ;;
esac
