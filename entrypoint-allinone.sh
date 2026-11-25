#!/usr/bin/env bash
set -euo pipefail

# Ensure persistent data and temp directories exist under /data so hardened
# read_only: true Docker containers never fail trying to write to /tmp or /root.
YATA_DATA="${YATA_DATA:-/data/yatagarasu}"
SUWAYOMI_DIR="${SUWAYOMI_DIR:-/data/suwayomi}"
TMP_DIR="${YATA_TMP_DIR:-${YATA_DATA}/tmp}"
mkdir -p "${YATA_DATA}" "${SUWAYOMI_DIR}" "${TMP_DIR}"

export TMPDIR="${TMP_DIR}"
export TEMP="${TMP_DIR}"
export TMP="${TMP_DIR}"
export HOME="${SUWAYOMI_DIR}"

# Pre-configure Suwayomi-Server on first boot so it binds strictly to loopback,
# downloads chapters as CBZ for Karasu, and never tries to open a browser/tray.
SUWAYOMI_CONF="${SUWAYOMI_DIR}/server.conf"
if [ ! -f "${SUWAYOMI_CONF}" ]; then
  cat > "${SUWAYOMI_CONF}" <<'EOF'
server.ip = "127.0.0.1"
server.port = 4567
server.downloadAsCbz = true
server.excludeUnreadChapters = false
server.excludeNotStarted = false
server.initialOpenInBrowserEnabled = false
server.systemTrayEnabled = false
server.webUIEnabled = false
EOF
else
  if ! grep -q 'server\.excludeUnreadChapters' "${SUWAYOMI_CONF}"; then
    printf '\nserver.excludeUnreadChapters = false\n' >> "${SUWAYOMI_CONF}"
  fi
  if ! grep -q 'server\.excludeNotStarted' "${SUWAYOMI_CONF}"; then
    printf 'server.excludeNotStarted = false\n' >> "${SUWAYOMI_CONF}"
  fi
fi

export YATA_DATA
export YATA_SUWAYOMI_URL="${YATA_SUWAYOMI_URL:-http://127.0.0.1:4567}"

PIDS=()

cleanup() {
  local sig="${1:-TERM}"
  echo "yatagarasu-allinone: received SIG${sig}, shutting down child processes..."
  for pid in "${PIDS[@]}"; do
    if kill -0 "${pid}" 2>/dev/null; then
      kill -TERM "${pid}" 2>/dev/null || true
    fi
  done
  for pid in "${PIDS[@]}"; do
    wait "${pid}" 2>/dev/null || true
  done
}

trap 'cleanup TERM; exit 0' TERM
trap 'cleanup INT; exit 0' INT

# Start bundled Suwayomi-Server in the background unless explicitly disabled.
if [ "${ENABLE_SUWAYOMI:-true}" = "true" ] && [ -f /opt/suwayomi/Suwayomi-Server.jar ]; then
  # shellcheck disable=SC2086
  java ${JAVA_OPTS:--Xms128m -Xmx512m} \
    -Djava.awt.headless=true \
    -Djava.io.tmpdir="${TMP_DIR}" \
    -Duser.home="${SUWAYOMI_DIR}" \
    -Dsuwayomi.tachidesk.config.server.rootDir="${SUWAYOMI_DIR}" \
    -jar /opt/suwayomi/Suwayomi-Server.jar &
  PIDS+=("$!")
  echo "yatagarasu-allinone: started Suwayomi-Server (pid $!) on 127.0.0.1:4567"
fi

# Start optional manga-image-translator if built into the image and enabled.
if [ "${ENABLE_IMAGE_TRANSLATOR:-true}" = "true" ] && [ -x /opt/manga-translator/.venv/bin/python ]; then
  mkdir -p /data/translator-models /data/translator-results
  export YATA_IMAGE_TRANSLATOR_URL="${YATA_IMAGE_TRANSLATOR_URL:-http://127.0.0.1:8000}"
  TRANSLATOR_ARGS=(
    /opt/manga-translator/server/main.py
    --host=127.0.0.1
    --port=8000
    --start-instance
    --models-ttl="${IMAGE_TRANSLATOR_MODELS_TTL:-300}"
  )
  if [ "${IMAGE_TRANSLATOR_GPU:-false}" = "true" ]; then
    TRANSLATOR_ARGS+=(--use-gpu)
  fi
  (cd /opt/manga-translator && exec /opt/manga-translator/.venv/bin/python "${TRANSLATOR_ARGS[@]}") &
  PIDS+=("$!")
  echo "yatagarasu-allinone: started manga-image-translator (pid $!) on 127.0.0.1:8000"
fi

# Start Yatagarasu as the unified front door on :3080.
/usr/local/bin/yatagarasu &
YATA_PID="$!"
PIDS+=("${YATA_PID}")

# Wait for Yatagarasu; if it exits unexpectedly, tear down background services too.
set +e
wait "${YATA_PID}"
STATUS=$?
set -e

cleanup TERM
exit "${STATUS}"
