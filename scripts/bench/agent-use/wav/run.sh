#!/usr/bin/env bash
# WebArena-Verified on this Linux computer for the agent-use benchmark, fully automatic. Same model, same prompt, same
# turn limit for every tool layer; only the browser tool differs. The user's Chrome, display and daemon are not used.
#
#   wav/run.sh --tasks FILE [--sides mewla,ab] [--sites shopping_admin,reddit,...] [--only wav-11,wav-67] [--tag label]
#              [--max-turns 40] [--cost-stop 50] [--keep] [--no-convert] [--warm]  (AUB_STAMP fixes the run id across invocations)
#
# FILE is `wav.py sample`'s output. Per site, in turn: the site container comes up (memory-capped, on a private port)
# behind proxy.py, which records the HAR the checker reads; each side runs that site's tasks; the container goes down.
# One container at a time. Sides: mewla = mewla_chrome (the real chain of web/run.sh, a person typing in another window:
# the non-interference probe), ab = agent-browser over CDP in the same Chrome, bu = browser-use's MCP server over CDP in the
# same Chrome (its two model-calling tools off), vscreen = the virtual-screen browser of daemon/browser behind an isolated
# daemon (vscreen.sh; its own display, so no probe). Needs the images of
# am1n3e/webarena-verified-<site> (docker pull) and `pip install webarena-verified` in a venv: WAV_VENV (default
# $MEWLA_BUILD_TMPDIR/wav/venv, made on first use).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../../../.." && pwd)
WARM=""; TASKS=""; SIDES="mewla,ab"; SITES=""; ONLY=""; TAG=""; MAXTURNS=40; COSTSTOP=50; KEEP=""; WM=kwin; CONVERT=1
while [ $# -gt 0 ]; do
  case $1 in
    --tasks) TASKS=$2; shift 2;;
    --sides) SIDES=$2; shift 2;;
    --sites) SITES=$2; shift 2;;
    --only) ONLY=$2; shift 2;;
    --tag) TAG=-$2; shift 2;;
    --max-turns) MAXTURNS=$2; shift 2;;
    --cost-stop) COSTSTOP=$2; shift 2;;
    --wm) WM=$2; shift 2;;
    --keep) KEEP=1; shift;;
    --no-convert) CONVERT=; shift;;
    --warm) WARM=1; shift;;
    *) echo "unknown flag $1" >&2; exit 2;;
  esac
done
[ -f "$TASKS" ] || { echo "--tasks FILE (from wav.py sample) is required" >&2; exit 2; }
TASKS=$(cd "$(dirname "$TASKS")" && pwd)/$(basename "$TASKS")
BUILD=${MEWLA_BUILD_TMPDIR:?MEWLA_BUILD_TMPDIR is not set}
VENV=${WAV_VENV:-$BUILD/wav/venv}
[ -x "$VENV/bin/python" ] || { uv venv -q --python 3.12 "$VENV" && uv pip install -q --python "$VENV/bin/python" webarena-verified; }
WORK=$(mktemp -d "$BUILD/aub-wav.XXXXXX")
RUN="$WORK/trials"; mkdir -p "$RUN"
DISP=${AUB_DISPLAY:-98}
STAMP=${AUB_STAMP:-$(date +%Y%m%d-%H%M)}
SHA=$(git -C "$REPO" rev-parse --short HEAD)
CTL=17999
LEDGER="$WORK/cost.ledger"
[ -n "$SITES" ] || SITES=$("$VENV/bin/python" -I -c "import json;print(','.join(json.load(open('$TASKS'))['sites']))")
UP=""
cleanup() {
  [ -d "$WORK/vs" ] && bash "$HERE/vscreen.sh" stop "$WORK/vs" >/dev/null 2>&1 || true
  touch "$WORK/bridge.json.stop" 2>/dev/null || true
  for s in ${UP}; do "$VENV/bin/python" "$HERE/wav.py" site down "$s" >/dev/null 2>&1 || true; done
  [ -f "$WORK/proxy.pid" ] && kill "$(cat "$WORK/proxy.pid")" 2>/dev/null || true
  sleep 3
  [ -f "$WORK/wm.pid" ] && kill "$(cat "$WORK/wm.pid")" 2>/dev/null || true
  [ -f "$WORK/xvnc.pid" ] && kill "$(cat "$WORK/xvnc.pid")" 2>/dev/null || true
  agent-browser close --all >/dev/null 2>&1 || true
  if [ -n "$KEEP" ]; then echo "kept $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

cc -O2 -o "$WORK/xuser" "$REPO/scripts/bench/agent-use/web/xuser.c" -lX11 -lXtst
PWV=$(node -e "console.log(require('$REPO/daemon/agentuse/playwright/package.json').dependencies['@playwright/mcp'])")
mkdir -p "$WORK/pwmcp" && (cd "$WORK/pwmcp" && npm init -y >/dev/null && npm i --no-audit --no-fund "@playwright/mcp@$PWV" >/dev/null 2>&1)
(cd "$REPO/daemon" && go build -o "$WORK/mewla" ./cmd/mewla)
"$VENV/bin/python" "$HERE/wav.py" site config --out "$WORK/config.json"

# The proxy publishes every site on the port its base URL names.
PROXY_SITES=()
for s in ${SITES//,/ }; do
  PROXY_SITES+=(--site "$("$VENV/bin/python" -I -c "
import importlib.util,sys
sp=importlib.util.spec_from_file_location('w','$HERE/wav.py');m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
v=m.SITES['$s'];print('%s:%d:%d%s'%('$s',v['public'],v['private'],(':'+v['login']) if v['login'] else ''))")")
done
nohup python3 -I "$HERE/proxy.py" "${PROXY_SITES[@]}" --ctl $CTL >"$WORK/proxy.log" 2>&1 &
echo $! >"$WORK/proxy.pid"

nohup Xvnc ":$DISP" -SecurityTypes None -localhost yes -rfbport $((59000 + DISP)) -geometry 1280x900 -depth 24 >"$WORK/xvnc.log" 2>&1 &
echo $! >"$WORK/xvnc.pid"; sleep 2
WMNOTE="no window manager (bare Xvnc)"
if [ "$WM" = kwin ]; then
  DISPLAY=":$DISP" KWIN_COMPOSE=N nohup kwin_x11 >"$WORK/wm.log" 2>&1 &
  echo $! >"$WORK/wm.pid"; sleep 4
  DISPLAY=":$DISP" xprop -root _NET_SUPPORTING_WM_CHECK >/dev/null 2>&1 || { echo "kwin_x11 did not come up; see $WORK/wm.log" >&2; KEEP=1; exit 1; }
  export XUSER_WM=1
  WMNOTE="kwin_x11 $(pacman -Q kwin 2>/dev/null | awk '{print $2}' || true), no compositing, default focus-stealing prevention"
fi
CFT=$(ls -d "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux64/chrome | tail -1)
(cd "$REPO/daemon" && DISPLAY=":$DISP" AUB_BRIDGE_OUT="$WORK/bridge.json" MEWLA_E2E_CHROME="$CFT" MEWLA_E2E_MCP="$WORK/pwmcp/node_modules/@playwright/mcp/cli.js" \
  MEWLA_E2E_BIN="$WORK/mewla" nohup go test ./agentuse -run AgentUseBenchBridge -v -count=1 -timeout 12h >"$WORK/bridge.log" 2>&1) &
for _ in $(seq 90); do [ -f "$WORK/bridge.json" ] && break; sleep 2; done
[ -f "$WORK/bridge.json" ] || { echo "the browser chain did not come up; see $WORK/bridge.log" >&2; KEEP=1; exit 1; }
EXT=$(python3 -I -c "import json;print(json.load(open('$WORK/bridge.json'))['extension_version'])")

case ",$SIDES," in *,vscreen,*)
  bash "$HERE/vscreen.sh" start "$WORK/vs"; . "$WORK/vs/env.sh"
  export VS_ENV_JSON="$WORK/vs/env.json" VS_RESTART="bash $HERE/vscreen.sh restart $WORK/vs";;
esac
case ",$SIDES," in *,bu,*)
  BUV=${BU_VENV:-$BUILD/bu/venv}
  [ -x "$BUV/bin/python" ] || { uv venv -q --python 3.12 "$BUV" && uv pip install -q --python "$BUV/bin/python" browser-use==0.13.14; }
  export BU_PY="$BUV/bin/python"; BUVER=$("$BUV/bin/python" -I -c "import importlib.metadata as m;print(m.version('browser-use'))");;
esac
export DISPLAY=":$DISP" BENCH_CDP=9334 BENCH_BRIDGE="$WORK/bridge.json" BENCH_XUSER="$WORK/xuser" BENCH_DIR="$WORK"
export WAV_TASKS="$TASKS" WAV_PY="$VENV/bin/python" WAV_CFG="$WORK/config.json" WAV_CTL=$CTL WAV_RUN="$RUN"
export WAV_WARM=${WARM:-0}
export AUB_COST_LEDGER="$LEDGER" AUB_COST_STOP="$COSTSTOP"
for site in ${SITES//,/ }; do
  IDS=$("$VENV/bin/python" -I -c "
import json
print(','.join('wav-%d'%t['id'] for t in json.load(open('$TASKS'))['tasks'] if t['site']=='$site'))")
  [ -n "$ONLY" ] && IDS=$(python3 -I -c "
a=set('$IDS'.split(','));b=set('$ONLY'.split(','));print(','.join(sorted(a&b)))")
  [ -n "$IDS" ] || continue
  UP="$UP $site"; "$VENV/bin/python" "$HERE/wav.py" site up "$site"
  for side in ${SIDES//,/ }; do
    OUT="$WORK/$site-$side.json"
    COMMON=(node "$HERE/../../browser/suite.mjs" --tasks "$HERE/tasks.mjs" --only "$IDS" --reps 1 --max-turns "$MAXTURNS" --trial-timeout 900 --side "$side" --out "$OUT")
    case $side in
      mewla)
        AUB_META="{\"run_id\":\"$STAMP-wav-hard-mewla_chrome-bg$TAG\",\"suite\":\"webarena-verified-hard\",\"transport\":\"Mewla extension $EXT + native host + bridge (real); supervise layer mirrored by mewla-proxy.mjs; display: $WMNOTE\",\"tool_version\":\"git:$SHA\",\"nonint_measured\":[\"key_focus_lost\",\"raised_over_user\",\"typing_lost\",\"pointer_moved\",\"user_page_hidden\"],\"nonint_not_measured\":[\"clipboard\",\"space_switch\",\"dialog_on_user\",\"app_activation\"]}" \
          "${COMMON[@]}" --nonint;;
      ab)
        AUB_META="{\"run_id\":\"$STAMP-wav-hard-agent-browser-fg$TAG\",\"suite\":\"webarena-verified-hard\",\"transport\":\"CDP attach to the same Chrome\",\"tool_version\":\"$(agent-browser --version 2>/dev/null | head -1)\"}" \
          "${COMMON[@]}";;
      bu)
        AUB_META="{\"run_id\":\"$STAMP-wav-hard-browser-use-fg$TAG\",\"suite\":\"webarena-verified-hard\",\"transport\":\"browser-use $BUVER MCP server (python -m browser_use.mcp) attached over CDP to the same Chrome; browser_extract_content and retry_with_browser_use_agent (they call a model of their own) disabled\",\"tool_version\":\"browser-use $BUVER\"}" \
          "${COMMON[@]}";;
      vscreen)
        AUB_META="{\"run_id\":\"$STAMP-wav-hard-mewla_virtual_screen$TAG\",\"suite\":\"webarena-verified-hard\",\"tool\":\"mewla_virtual_screen\",\"condition\":\"isolated-display\",\"transport\":\"mewla browser mcp (daemon/browser, Chrome on a private kwin_wayland virtual screen) behind an isolated daemon; one Worker held by a stand-in provider CLI\",\"tool_version\":\"git:$SHA\"}" \
          "${COMMON[@]}";;
      *) echo "side $side is not wired in this script yet" >&2; exit 2;;
    esac
  done
  "$VENV/bin/python" "$HERE/wav.py" site down "$site"; UP=${UP// $site/}
done
# One result file per side, the sites' parts merged.
for side in ${SIDES//,/ }; do
  PARTS=$(ls "$WORK"/*-"$side".json 2>/dev/null || true)
  [ -n "$PARTS" ] && [ -n "$CONVERT" ] && python3 -I "$HERE/../aub.py" convert-web --merge $PARTS
done
echo "model spend: \$$(awk '{s+=$1} END{printf "%.2f", s}' "$LEDGER" 2>/dev/null || echo 0)"
if [ -n "$KEEP" ]; then cp -r "$RUN" "$BUILD/wav/trials-$STAMP$TAG"; echo "trials kept in $BUILD/wav/trials-$STAMP$TAG"; fi
