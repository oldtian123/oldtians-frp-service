#!/usr/bin/env bash
#
# Installer for ofs-gateway-agent (Oldtian's FRP Service) on Linux / macOS.
#
# Usage (run as root / with sudo):
#   ./install.sh [path-to-ofs-gateway-agent-binary]
#
# It installs the binary to /usr/local/bin, sets up /etc/oldtians-frp-service,
# and (on Linux with systemd) installs + enables the service unit.
set -euo pipefail

PREFIX="/usr/local/bin"
CONF_DIR="/etc/oldtians-frp-service"
BIN_NAME="ofs-gateway-agent"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="${1:-${SCRIPT_DIR}/${BIN_NAME}}"

if [[ "${EUID}" -ne 0 ]]; then
  echo "error: please run as root (sudo ./install.sh)" >&2
  exit 1
fi

if [[ ! -f "${BIN_SRC}" ]]; then
  echo "error: agent binary not found at ${BIN_SRC}" >&2
  echo "       build it first: (cd agent && go build -o ${BIN_NAME} ./cmd/gateway-agent)" >&2
  exit 1
fi

echo ">> installing ${BIN_NAME} to ${PREFIX}"
install -m 0755 "${BIN_SRC}" "${PREFIX}/${BIN_NAME}"

echo ">> creating ${CONF_DIR}"
mkdir -p "${CONF_DIR}"

if [[ ! -f "${CONF_DIR}/config.yaml" ]]; then
  if [[ -f "${SCRIPT_DIR}/../config.example.yaml" ]]; then
    cp "${SCRIPT_DIR}/../config.example.yaml" "${CONF_DIR}/config.yaml"
    echo ">> wrote example config to ${CONF_DIR}/config.yaml -- EDIT IT before starting"
  else
    echo "!! config.example.yaml not found; create ${CONF_DIR}/config.yaml manually"
  fi
else
  echo ">> ${CONF_DIR}/config.yaml already exists, leaving it untouched"
fi

# frpc reminder
if ! command -v frpc >/dev/null 2>&1 && [[ ! -x /usr/local/bin/frpc ]]; then
  echo "!! frpc binary not found. Download it from https://github.com/fatedier/frp/releases"
  echo "   and place it at the bin_path configured in config.yaml (default /usr/local/bin/frpc)."
fi

OS="$(uname -s)"
if [[ "${OS}" == "Linux" ]] && command -v systemctl >/dev/null 2>&1; then
  echo ">> installing systemd service"
  install -m 0644 "${SCRIPT_DIR}/oldtians-frp-service.service" /etc/systemd/system/oldtians-frp-service.service
  systemctl daemon-reload
  systemctl enable oldtians-frp-service.service
  echo ">> service installed. Start it with:"
  echo "     systemctl start oldtians-frp-service"
  echo "     journalctl -u oldtians-frp-service -f"
else
  echo ">> no systemd detected (${OS}). Run the agent manually or via your init system:"
  echo "     ${PREFIX}/${BIN_NAME} -config ${CONF_DIR}/config.yaml"
fi

echo ">> done."
