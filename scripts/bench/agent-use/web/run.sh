#!/usr/bin/env bash
# Web suite of the agent-use benchmark, fully automatic, on this Linux computer. No window of the user's is used:
# its own Xvnc display, its own Chrome for Testing, its own throwaway state; nothing of the live daemon is touched.
#
#   web/run.sh [--reps 3] [--only id,id] [--with-long] [--sides mewla,ab] [--keep]
#
# mewla side: the real chain. daemon/agentuse TestAgentUseBenchBridge brings up Mewla's extension, native host and
#   bridge in a Chrome for Testing and writes the browser tools' argv exactly as the daemon builds them; the tools
#   run behind benchmarks' mewla-proxy.mjs (the tool advice and snapshot compaction of daemon/agentuse, mirrored),
#   as `claude -p --tools ""` with only mewla_chrome, claude-sonnet-5-5. A person (xuser.c) types into a window of
#   their own all the while: that is the non-interference probe (suite.mjs --nonint).
# ab side: agent-browser on the same Chrome through CDP, success and speed only (it has no window of its own, so
#   the probe would measure the harness, not the tool).
# Writes results/<run>.json through aub.py. Needs: node, go, cc + libX11/libXtst headers, Xvnc, claude, network
# (npm i @playwright/mcp at the version daemon/agentuse/playwright/package.json pins).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../../../.." && pwd)
REPS=3; ONLY=""; SIDES="mewla,ab"; LONG=""; KEEP=""
while [ $# -gt 0 ]; do
  case $1 in
    --reps) REPS=$2; shift 2;;
    --only) ONLY=$2; shift 2;;
    --sides) SIDES=$2; shift 2;;
    --with-long) LONG="--with-long"; shift;;
    --keep) KEEP=1; shift;;
    *) echo "unknown flag $1" >&2; exit 2;;
  esac
done
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aub-web.XXXXXX")
DISP=${AUB_DISPLAY:-98}
STAMP=$(date +%Y%m%d-%H%M)
SHA=$(git -C "$REPO" rev-parse --short HEAD)
cleanup() {
  touch "$WORK/bridge.json.stop" 2>/dev/null || true
  sleep 3
  [ -f "$WORK/xvnc.pid" ] && kill "$(cat "$WORK/xvnc.pid")" 2>/dev/null || true
  agent-browser close --all >/dev/null 2>&1 || true
  [ -n "$KEEP" ] || rm -rf "$WORK"
}
trap cleanup EXIT

cc -O2 -o "$WORK/xuser" "$HERE/xuser.c" -lX11 -lXtst
PWV=$(node -e "console.log(require('$REPO/daemon/agentuse/playwright/package.json').dependencies['@playwright/mcp'])")
mkdir -p "$WORK/pwmcp" && (cd "$WORK/pwmcp" && npm init -y >/dev/null && npm i --no-audit --no-fund "@playwright/mcp@$PWV" >/dev/null 2>&1)
(cd "$REPO/daemon" && go build -o "$WORK/mewla" ./cmd/mewla)
nohup Xvnc ":$DISP" -SecurityTypes None -localhost yes -rfbport $((59000 + DISP)) -geometry 1280x900 -depth 24 >"$WORK/xvnc.log" 2>&1 &
echo $! >"$WORK/xvnc.pid"; sleep 2
CFT=$(ls -d "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux64/chrome | tail -1)
(cd "$REPO/daemon" && DISPLAY=":$DISP" AUB_BRIDGE_OUT="$WORK/bridge.json" MEWLA_E2E_CHROME="$CFT" MEWLA_E2E_MCP="$WORK/pwmcp/node_modules/@playwright/mcp/cli.js" \
  MEWLA_E2E_BIN="$WORK/mewla" nohup go test ./agentuse -run AgentUseBenchBridge -v -count=1 -timeout 3h >"$WORK/bridge.log" 2>&1) &
for _ in $(seq 90); do [ -f "$WORK/bridge.json" ] && break; sleep 2; done
[ -f "$WORK/bridge.json" ] || { echo "the browser chain did not come up; see $WORK/bridge.log" >&2; KEEP=1; exit 1; }
EXT=$(python3 -I -c "import json;print(json.load(open('$WORK/bridge.json'))['extension_version'])")

export DISPLAY=":$DISP" BENCH_CDP=9334 BENCH_BRIDGE="$WORK/bridge.json" BENCH_XUSER="$WORK/xuser" BENCH_DIR="$WORK"
SUITE=(node "$HERE/../../browser/suite.mjs" --reps "$REPS" $LONG)
[ -n "$ONLY" ] && SUITE+=(--only "$ONLY")
# Extra suite.mjs flags for debugging, e.g. AUB_SUITE_EXTRA="--keep /tmp/dir" keeps each trial's stream-json.
[ -n "${AUB_SUITE_EXTRA:-}" ] && SUITE+=($AUB_SUITE_EXTRA)
for side in ${SIDES//,/ }; do
  if [ "$side" = mewla ]; then
    AUB_META="{\"run_id\":\"$STAMP-web-fixture-mewla_chrome-bg\",\"transport\":\"Mewla extension $EXT + native host + bridge (real); supervise layer mirrored by mewla-proxy.mjs\",\"tool_version\":\"git:$SHA\",\"nonint_measured\":[\"key_focus_lost\",\"raised_over_user\",\"typing_lost\",\"pointer_moved\",\"user_page_hidden\"],\"nonint_not_measured\":[\"clipboard\",\"space_switch\",\"dialog_on_user\",\"app_activation\"]}" \
      "${SUITE[@]}" --side mewla --nonint --out "$WORK/mewla.json"
    python3 -I "$HERE/../aub.py" convert-web "$WORK/mewla.json"
  else
    AUB_META="{\"run_id\":\"$STAMP-web-fixture-agent-browser-fg\",\"transport\":\"CDP attach to the same Chrome\",\"tool_version\":\"$(agent-browser --version 2>/dev/null | head -1)\"}" \
      "${SUITE[@]}" --side ab --out "$WORK/ab.json"
    python3 -I "$HERE/../aub.py" convert-web "$WORK/ab.json"
  fi
done
