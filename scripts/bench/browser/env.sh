#!/usr/bin/env bash
# Isolated benchmark environment: own X display (Xvnc), own Chrome for Testing
# profile with the Playwright extension (built from source), CDP on :9333 for
# agent-browser. Never touches the user's Chrome or display.
#   env.sh up      start (idempotent), print the extension key
#   env.sh down    stop and remove
# State lives in $BENCH_DIR (default $TMPDIR/bench).
set -euo pipefail
BENCH_DIR=${BENCH_DIR:-${TMPDIR:-/tmp}/bench}
DISPLAY_NUM=${BENCH_DISPLAY:-97}
CDP_PORT=${BENCH_CDP:-9333}
CFT=${BENCH_CHROME:-$(ls -d "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux64/chrome | tail -1)}
EXT_ID=mmlmfjhmonkocbjadbfplnigmagldckm
HERE=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$BENCH_DIR"

case ${1:-up} in
up)
  if ! DISPLAY=:$DISPLAY_NUM xdpyinfo >/dev/null 2>&1; then
    nohup Xvnc ":$DISPLAY_NUM" -SecurityTypes None -localhost yes -rfbport $((59000 + DISPLAY_NUM)) -geometry 1280x900 -depth 24 >"$BENCH_DIR/xvnc.log" 2>&1 &
    echo $! >"$BENCH_DIR/xvnc.pid"; sleep 2
  fi
  if [ ! -f "$BENCH_DIR/ext/dist/manifest.json" ]; then
    mkdir -p "$BENCH_DIR/ext"
    git clone --depth 1 --filter=blob:none --sparse https://github.com/microsoft/playwright.git "$BENCH_DIR/ext/src" >/dev/null 2>&1
    (cd "$BENCH_DIR/ext/src" && git sparse-checkout set packages/extension >/dev/null 2>&1)
    (cd "$BENCH_DIR/ext/src/packages/extension" && npm init -y >/dev/null 2>&1 &&
      npm i --no-audit --no-fund vite @vitejs/plugin-react vite-plugin-static-copy react react-dom @types/chrome @types/react @types/react-dom typescript >/dev/null 2>&1 &&
      npx vite build --app >/dev/null 2>&1)
    mkdir -p "$BENCH_DIR/ext" && rm -rf "$BENCH_DIR/ext/dist" && cp -r "$BENCH_DIR/ext/src/packages/extension/dist" "$BENCH_DIR/ext/dist"
  fi
  if ! curl -s -m1 "localhost:$CDP_PORT/json/version" >/dev/null; then
    # Throttling flags: a headed Chrome on a bare display paints rarely, and
    # Playwright's actionability waits for animation frames. Same flags for
    # every tool, so no side is favoured.
    DISPLAY=:$DISPLAY_NUM nohup "$CFT" --user-data-dir="$BENCH_DIR/profile" --remote-debugging-port="$CDP_PORT" \
      --load-extension="$BENCH_DIR/ext/dist" --no-first-run --no-default-browser-check --no-sandbox \
      --disable-renderer-backgrounding --disable-background-timer-throttling --disable-backgrounding-occluded-windows \
      --disable-features=CalculateNativeWinOcclusion about:blank >"$BENCH_DIR/chrome.log" 2>&1 &
    echo $! >"$BENCH_DIR/chrome.pid"
    for _ in $(seq 40); do curl -s -m1 "localhost:$CDP_PORT/json/version" >/dev/null && break; sleep 0.25; done
  fi
  cat >"$BENCH_DIR/chrome-wrap.sh" <<WRAP
#!/bin/bash
exec "$CFT" --user-data-dir="$BENCH_DIR/profile" --no-sandbox "\$@"
WRAP
  chmod +x "$BENCH_DIR/chrome-wrap.sh"
  # The extension's key, read from its own status page.
  AGENT_BROWSER_SESSION=envkey agent-browser --cdp "$CDP_PORT" open "chrome-extension://$EXT_ID/status.html" >/dev/null
  KEY=$(AGENT_BROWSER_SESSION=envkey agent-browser get text body | grep -o 'PLAYWRIGHT_MCP_EXTENSION_TOKEN=[A-Za-z0-9_=-]*' | cut -d= -f2-)
  AGENT_BROWSER_SESSION=envkey agent-browser close >/dev/null 2>&1 || true
  echo "$KEY" >"$BENCH_DIR/ext-key"
  echo "display :$DISPLAY_NUM cdp $CDP_PORT key-file $BENCH_DIR/ext-key"
  ;;
down)
  for f in chrome xvnc; do [ -f "$BENCH_DIR/$f.pid" ] && kill "$(cat "$BENCH_DIR/$f.pid")" 2>/dev/null || true; done
  sleep 1; rm -rf "$BENCH_DIR/profile" "$BENCH_DIR"/*.pid
  ;;
esac
