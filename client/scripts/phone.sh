#!/usr/bin/env bash
# Lane 2, one warm Android phone for everything only Android shows
# (share sheet and intent filters, permissions, notifications, keyboard and insets, lifecycle).
#
# The phone is the AVD `mewla-lane` (API 35, 2.5 GB RAM) with the Dev app (preview debug) installed, paired to an
# isolated test daemon of its own (fixed state dir, loopback port, inert Brain host, never the live daemon) and saved as
# the snapshot `ready`. UI changes reach it by hot reload (an `nsh dev --release` server on this host), a Kotlin
# change by `phone.sh install` (incremental Gradle, no clean build). Its own lock ($MEWLA_LANE_HOME/emulator.lock)
# allows one lane emulator at a time while it runs (a `timeout` caps that), and `up` refuses to start when less than
# 8 GiB of memory is available. The heavy lock is not taken by the emulator; only the Gradle build of `install` takes it.
#
#   phone.sh up          boot from the snapshot (refuses below 8 GiB of free memory or when one is running), start the test daemon and the dev server
#   phone.sh reset       the app stopped and started again: Brain open, connected (about 4 s). Not restored: what the app and the
#                        phone keep (drafts, other apps, files, the test daemon's messages); flows send fresh text and names
#   phone.sh restore     back to the snapshot's state (paired, Brain open). About 11 s, but about 1 in 20 takes 50-150 s (see README)
#   phone.sh shot [F]    screenshot to F (default: a fresh png under $TMPDIR), prints the path
#   phone.sh tap REGEX   tap the first node whose text, description or class matches (uiautomator), waits up to 20 s
#   phone.sh scroll      one swipe up through the middle of the screen (a row below the fold comes in)
#   phone.sh shows REGEX [SECS]  exit 0 once the screen has such a node (waits up to SECS, default 10)
#   phone.sh dump        the screen as text, one line per node with an id, text or description (what to read instead of a screenshot or the XML)
#   phone.sh share-file PATH [MIME]   share a file into Mewla (ACTION_SEND of a MediaStore content:// URI; about 1 s; open-with: untested)
#   phone.sh share-text TEXT          share text into Mewla
#   phone.sh open-with PATH [MIME]    "Open with Mewla" a file (ACTION_VIEW)
#   phone.sh install     incremental debug build + install of the Dev app (a Kotlin or manifest change)
#   phone.sh save        save the current state as the snapshot `ready` (after: install, dev-link + tap Load, pair + tap Pair, Open Brain)
#   phone.sh status      what is running
#   phone.sh down        stop the emulator (releases the lock), the daemon and the dev server
#   phone.sh type TEXT   tap the composer and type
#   phone.sh flow [NAME..|all]   reset, then run flows/NAME.sh (a journey as a file); prints PASS or FAIL and the seconds
#   phone.sh corpus OUT [NAME..]  the terminal render corpus (scripts/terminal-corpus) replayed into the Dev app's TerminalView, one PNG per
#                        screen in OUT (FONT=<dp>, LIGHT=1); a screen that does not come up is reported and fails. Needs the debug build
#                        with TerminalReplayActivity (`install` after a Kotlin change); `reset` brings Brain back
#   phone.sh run VERB..  up, then the verbs (a `-- ` separates each), then down: a whole check, lock held only meanwhile
#
# Everything is under $MEWLA_LANE_HOME (default ~/.mewla/lane): the daemon binary (built from this checkout
# by `phone.sh daemon-build`), its state, pid files. Environment: LANE_MIN_GIB (memory to start, 8), LANE_MAX_MIN (hard cap
# on the emulator's life, 30), DEV_PORT (8899; never 8790, the owner's), DAEMON_PORT (9951), SERIAL (emulator-5584).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
client="$(cd "$here/.." && pwd)"
repo="$(cd "$client/.." && pwd)"
lane="${MEWLA_LANE_HOME:-$HOME/.mewla/lane}"
lock="$lane/emulator.lock"
heavy_lock="${LANE_HEAVY_LOCK:-$HOME/.mewla/t/0g-heavy.lock}"
avd="${LANE_AVD:-mewla-lane}"
console_port="${SERIAL_PORT:-5584}"
serial="emulator-$console_port"
daemon_port="${DAEMON_PORT:-9951}"
dev_port="${DEV_PORT:-8899}"
snapshot="ready"
emulator="${ANDROID_HOME:-$HOME/Android/Sdk}/emulator/emulator"
nsh_home="${NSH_HOME:-$HOME/.nsh}"
pkg="com.daoleno.mewla.preview.debug"
apk="$client/android/app/build/outputs/apk/preview/debug/app-preview-debug.apk"
# PHONE_EXTERNAL=1: the phone, the app and the test daemon are the caller's (the release gate's emulator and the exact signed APK,
# release-flows.sh): PHONE_PKG is the app's package, ANDROID_SERIAL its device, and only the verbs that act on a booted, paired app
# work (reset, wait-connected, shot, tap, scroll, shows, dump, type, flow, share-file, share-text, open-with).
external="${PHONE_EXTERNAL:-}"
if [[ -n "$external" ]]; then pkg="${PHONE_PKG:?PHONE_EXTERNAL needs PHONE_PKG}"; serial="${ANDROID_SERIAL:?PHONE_EXTERNAL needs ANDROID_SERIAL}"; fi
export ANDROID_SERIAL="$serial"
mkdir -p "$lane/bin" "$lane/state" "$lane/home"

log() { echo "[$(date +%T)] $*" >&2; }
die() { log "phone.sh: $*"; exit 1; }
alive() { [[ -f "$1" ]] && kill -0 "$(cat "$1")" 2>/dev/null; }
mewla() { env -i HOME="$lane/home" PATH="$PATH" "$lane/mewla" "$@"; }

# ---- the emulator -------------------------------------------------------------------------------------------
boot_done() { [[ "$(adb -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == 1 ]]; }

emu_up() {
  if alive "$lane/emulator.pid" && boot_done; then return 0; fi
  [[ -x "$emulator" ]] || die "no emulator at $emulator (ANDROID_HOME)"
  avail=$(awk '/MemAvailable/ {print int($2/1048576)}' /proc/meminfo)
  ((avail >= ${LANE_MIN_GIB:-8})) || die "only ${avail} GiB of memory available (want ${LANE_MIN_GIB:-8}+): not starting the lane emulator"
  (flock -n 8) 8> "$lock" || die "another lane emulator is running (one at a time; phone.sh down stops it)"
  local mode=(-no-snapshot-load)
  [[ -d "$HOME/.android/avd/$avd.avd/snapshots/$snapshot" && -z "${COLD:-}" ]] && mode=(-snapshot "$snapshot")
  log "booting $avd (${mode[*]}), lane lock $lock"
  # The lock is held by the emulator process itself (flock execs it, `timeout` caps its life): when it exits, the
  # lock is free. The daemon and the dev server are started elsewhere, never with this lock's descriptor.
  : > "$lane/emulator.log"
  # setsid may fork: the shell that execs flock writes its own pid, which stays the pid of the process holding the lock.
  setsid -f sh -c 'echo $$ > "$0"; exec "$@"' "$lane/emulator.pid" \
    flock -n "$lock" \
    timeout --signal=TERM $(( ${LANE_MAX_MIN:-30} * 60 )) \
    "$emulator" -avd "$avd" -port "$console_port" -no-window -no-audio -no-boot-anim -no-metrics \
      -gpu swiftshader_indirect -memory 2560 -cores 4 "${mode[@]}" -no-snapshot-save \
    > "$lane/emulator.log" 2>&1 < /dev/null
  for _ in $(seq 1 20); do [[ -s "$lane/emulator.pid" ]] && break; sleep 0.1; done
  local deadline=$((SECONDS + 240))
  until boot_done; do
    alive "$lane/emulator.pid" || die "the emulator did not start (the lane lock was taken, or a crash): $(tail -3 "$lane/emulator.log")"
    ((SECONDS < deadline)) || die "the emulator did not boot in time"
    sleep 2
  done
  log "$serial booted"
}

emu_down() {
  if alive "$lane/emulator.pid"; then
    kill "$(cat "$lane/emulator.pid")" 2>/dev/null || true
    for _ in $(seq 1 30); do alive "$lane/emulator.pid" || break; sleep 1; done
    alive "$lane/emulator.pid" && kill -9 "$(cat "$lane/emulator.pid")" 2>/dev/null || true
  fi
  rm -f "$lane/emulator.pid"
  # A second `up` or a reset that was cut short can leave the pid file pointing at a dead wrapper while this AVD's emulator runs on.
  local p
  for p in $(pgrep -f "emulator/emulator -avd $avd " 2>/dev/null); do kill "$p" 2>/dev/null || true; done
}


# ---- the test daemon (own state, loopback, inert Brain host) ------------------------------------------------
daemon_build() {
  # -tags ghosttyvt: the session host (Brain's) exits at start without the terminal engine.
  local gh="${GHOSTTY_DESKTOP:-$HOME/.cache/mewla/ghostty-desktop}"
  (cd "$repo/daemon" && GOTMPDIR="${MEWLA_BUILD_TMPDIR:-${TMPDIR:-/tmp}}" CGO_ENABLED=1 CGO_CFLAGS="-I$gh/include" \
    CGO_LDFLAGS="-L$gh/linux_amd64" go build -tags ghosttyvt -o "$lane/mewla" ./cmd/mewla)
}


daemon_up() {
  [[ -x "$lane/mewla" ]] || daemon_build
  if alive "$lane/daemon.pid"; then return 0; fi
  # An inert Brain host (a cursor-agent that answers nothing) and no systemd-run: the daemon's session host is then
  # its own child, never a user unit that could outlive it holding what its caller held (the emulator lock).
  printf '#!/bin/sh\n[ "$1" = --version ] && { echo 0.0.0-lane; exit 0; }\nexec sleep 86400\n' > "$lane/bin/cursor-agent"
  printf '#!/bin/sh\necho "systemd-run is off for the lane daemon" >&2\nexit 1\n' > "$lane/bin/systemd-run"
  chmod +x "$lane/bin/cursor-agent" "$lane/bin/systemd-run"
  # The Brain host runs the stub by its absolute path: by name, a login shell's PATH can find the owner's real
  # cursor-agent first (0.4.28 gate: it ran signed out and wrote ~/.cursor/projects/<scratch state>).
  mkdir -p "$lane/state"
  # The lane's state lasts from run to run: the entry is added once, beside whatever else a proof put there.
  grep -qs '^name = "agent"$' "$lane/state/executors.toml" ||
    printf '\n[[executors]]\nname = "agent"\ncommand = "%s"\nkind = "cursor"\n' "$lane/bin/cursor-agent" >> "$lane/state/executors.toml"
  touch "$lane/home/.zshrc"
  # setsid may fork: the shell that execs the daemon writes its own pid, which stays the daemon's.
  # `setsid -f` forks and returns at once: a backgrounded subshell would stay behind as the server's parent and hold the
  # caller's stdout (a `| tail` of this script then never ends). The shell that execs the server writes its own pid.
  (cd "$lane/home" && exec setsid -f sh -c 'exec 9>&-; echo $$ > "$0"; exec "$@"' "$lane/daemon.pid" env -i HOME="$lane/home" PATH="$lane/bin:$PATH" TERM=xterm-256color \
    MEWLA_BRAIN_HOST_EXECUTOR=agent MEWLA_NO_AUTOUPDATE=1 \
    "$lane/mewla" serve -addr "127.0.0.1:$daemon_port" -loopback-only -state-dir "$lane/state" \
    > "$lane/daemon.log" 2>&1 < /dev/null)
  for _ in $(seq 1 60); do (exec 3<>"/dev/tcp/127.0.0.1/$daemon_port") 2>/dev/null && return 0; sleep 1; done
  die "the test daemon did not listen (see $lane/daemon.log)"
}

daemon_down() {
  if [[ -x "$lane/mewla" ]]; then
    mewla host status --state-dir "$lane/state" --json > "$lane/host.json" 2>/dev/null || true
    # The session host outlives serve: stop it and its Sessions by the pids it reports.
    local pids
    pids=$(python3 -I -c 'import json,sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
print(" ".join(str(s["pid"]) for s in d.get("sessions") or []), (d.get("host") or {}).get("pid", ""))' "$lane/host.json" 2>/dev/null || true)
    alive "$lane/daemon.pid" && kill "$(cat "$lane/daemon.pid")" 2>/dev/null || true
    for _ in $(seq 1 20); do alive "$lane/daemon.pid" || break; sleep 0.5; done
    # serve can ignore SIGTERM for over a minute; this is scratch state, so it is not waited for.
    alive "$lane/daemon.pid" && kill -9 "$(cat "$lane/daemon.pid")" 2>/dev/null || true
    # shellcheck disable=SC2086
    [[ -z "${pids// }" ]] || kill $pids 2>/dev/null || true
  fi
  rm -f "$lane/daemon.pid"
}

# ---- the dev server (hot reload) ------------------------------------------------------------------------------
dev_up() {
  if alive "$lane/dev.pid"; then return 0; fi
  [[ "$dev_port" != 8790 ]] || die "8790 is the owner's dev server: never start one there"
  # --release: the full app's debug bundle is 16 MB of bytecode, which the Dev app resets mid-transfer.
  (exec setsid -f sh -c 'exec 9>&-; echo $$ > "$0"; exec "$@"' "$lane/dev.pid" "$nsh_home/bin/nsh" dev --release --server-only --port "$dev_port" "$client/ui" \
    > "$lane/dev.log" 2>&1 < /dev/null)
  for _ in $(seq 1 60); do (exec 3<>"/dev/tcp/127.0.0.1/$dev_port") 2>/dev/null && return 0; sleep 0.5; done
  die "nsh dev did not listen (see $lane/dev.log)"
}

dev_down() {
  alive "$lane/dev.pid" && kill "$(cat "$lane/dev.pid")" 2>/dev/null || true
  rm -f "$lane/dev.pid"
}

# ---- the screen -----------------------------------------------------------------------------------------------
screen() { adb -s "$serial" shell uiautomator dump /sdcard/lane.xml > /dev/null 2>&1; adb -s "$serial" shell cat /sdcard/lane.xml; }
# Waits up to SECONDS (default 10) for the screen to show REGEX: the app draws a moment after a tap, so one look right after is too early.
shows() {
  local deadline=$((SECONDS + ${2:-10}))
  while ! screen | grep -qiE "$1"; do ((SECONDS < deadline)) || return 1; done
}
# What a screen holds, one line per node that has an id, a text or a description (a screen is 20 KB of XML; this is under 1 KB).
dump() {
  screen | python3 -I -c '
import re, sys
for n in re.findall(r"<node [^>]*>", sys.stdin.read()):
    v = [re.search(a + "=\"([^\"]*)\"", n).group(1) for a in ("resource-id", "text", "content-desc")]
    if any(v): print(" | ".join(x for x in v if x))
'
}
tap() {
  local deadline=$((SECONDS + ${2:-20})) at
  while ((SECONDS < deadline)); do
    at=$(screen | python3 -I -c '
import re, sys
xml, pattern = sys.stdin.read(), re.compile(sys.argv[1], re.I)
for node in re.finditer(r"<node [^>]*>", xml):
    n = node.group(0)
    values = re.findall(r"(?:text|content-desc|resource-id|class)=\"([^\"]*)\"", n)
    box = re.search(r"bounds=\"\[(\d+),(\d+)\]\[(\d+),(\d+)\]\"", n)
    if box and any(pattern.search(v) for v in values):
        x1, y1, x2, y2 = map(int, box.groups()); print((x1 + x2) // 2, (y1 + y2) // 2); break
' "$1")
    if [[ -n "$at" ]]; then adb -s "$serial" shell input tap $at; return 0; fi
    sleep 1
  done
  return 1
}
shot() {
  local f="${1:-$(mktemp "${TMPDIR:-/tmp}/phone.XXXXXX").png}"
  adb -s "$serial" exec-out screencap -p > "$f"
  echo "$f"
}

# ---- what other apps do ---------------------------------------------------------------------------------------
# A real sender (Files, Chrome, WeChat) hands Mewla a content:// URI it grants for the share, never a file path
# (a file:// path is "Cannot read this file" to Mewla, as to any app without storage access). The file goes into the
# phone's MediaStore, which gives it a content:// URI; `am start` grants a URI given as the intent's data.
mime_of() { case "${1##*.}" in png) echo image/png ;; jpg|jpeg) echo image/jpeg ;; pdf) echo application/pdf ;; txt) echo text/plain ;; docx) echo application/vnd.openxmlformats-officedocument.wordprocessingml.document ;; *) echo application/octet-stream ;; esac; }
content_uri_of() { # PATH -> content:// URI of its MediaStore row
  local name dir dest id
  name="$(basename "$1")"
  # A directory of its own per share: a new MediaStore row each time (an old row of the same path was refused a grant).
  case "$(mime_of "$1")" in image/*) dir="/sdcard/Pictures/lane-$(date +%s%N)" ;; *) dir="/sdcard/Download/lane-$(date +%s%N)" ;; esac
  dest="$dir/$name"
  adb -s "$serial" shell mkdir -p "$dir"
  adb -s "$serial" push "$1" "$dest" > /dev/null
  # MediaStore keeps the path as /storage/emulated/0/..., not the /sdcard/... an adb push used: a query on the /sdcard
  # spelling matches nothing, which is why this lookup once timed out. The index of a pushed file is asynchronous, so
  # wait for the row (a scan request first), not for a second attempt.
  local dbpath="/storage/emulated/0${dest#/sdcard}"
  adb -s "$serial" shell am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d "file://$dest" > /dev/null 2>&1 || true
  for _ in $(seq 1 40); do
    id="$(adb -s "$serial" shell content query --uri content://media/external/file --projection _id --where "_data=\\'$dbpath\\'" 2>/dev/null | grep -o '_id=[0-9]*' | tail -1 | cut -d= -f2)"
    [[ -z "$id" ]] || { echo "content://media/external/file/$id"; return 0; }
    sleep 0.25
  done
  die "MediaStore did not index $dest"
}
# A share started by a root adb is refused: "the system cannot issue a Uri permission grant" (UriGrantsManagerService), the app
# shows "Cannot read this file". Everything that hands a URI to the app runs as the shell user.
as_shell() { [[ "$(adb -s "$serial" shell id -u | tr -d '\r')" == 2000 ]] || die "adb is not the shell user (adb root left on): a URI grant would be refused; run adb unroot"; }
share_file() {
  local uri mime="${2:-$(mime_of "$1")}"
  as_shell
  uri="$(content_uri_of "$1")"
  adb -s "$serial" shell am start -W -n "$pkg/com.daoleno.mewla.MainActivity" -a android.intent.action.SEND -t "$mime" \
    -d "$uri" --eu android.intent.extra.STREAM "$uri" --grant-read-uri-permission > /dev/null
}
share_text() {
  adb -s "$serial" shell am start -W -n "$pkg/com.daoleno.mewla.MainActivity" -a android.intent.action.SEND -t text/plain \
    --es android.intent.extra.TEXT "'$1'" > /dev/null
}
open_with() {
  local uri mime="${2:-$(mime_of "$1")}"
  as_shell
  uri="$(content_uri_of "$1")"
  adb -s "$serial" shell am start -W -n "$pkg/com.daoleno.mewla.MainActivity" -a android.intent.action.VIEW -t "$mime" -d "$uri" --grant-read-uri-permission > /dev/null
}

install_app() {
  # Incremental: Gradle's own up-to-date checks; never `clean`. --no-daemon and a memory cap: the host is shared.
  (cd "$client/android" && JAVA_HOME="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk}" NSH_HOME="$nsh_home" PATH="$nsh_home/bin:$PATH" \
    flock -w 3600 "$heavy_lock" systemd-run --user --scope -q -p MemoryMax=6G -p CPUQuota=800% \
    ./gradlew --no-daemon -q assemblePreviewDebug -Pmewla.abis=x86_64) >&2
  [[ -f "$apk" ]] || die "no APK at $apk"
}
install_on_device() { adb -s "$serial" install -r -g "$apk" 2>&1 | tail -1 >&2; }

# The Dev app opens on a dev server it is told about (mewla-dev://start?server=…): the emulator reaches this host as 10.0.2.2.
point_at_dev() {
  adb -s "$serial" shell am start -a android.intent.action.VIEW -d "'mewla-dev://start?server=ws://10.0.2.2:$dev_port/ws'" "$pkg" > /dev/null
}

# A pairing link (the code is single-use and dies after about 30 minutes: make it right before it is used).
# host: 10.0.2.2 for the emulator, 127.0.0.1 for a browser on this machine.
pair_link_for() { mewla pair -details -state-dir "$lane/state" "http://$1:$daemon_port" 2>&1 | grep -o 'mewla://settings?p=[^ ]*' | head -1; }

# ---- after a snapshot is restored ------------------------------------------------------------------------------
# A restored snapshot repeats itself: the app process in it signs the very requests it signed the first time after the
# snapshot (same random state), and the daemon's replay guard refuses them ("reason=replay"; later "Pair again"); a synced
# clock does not change that (the nonce repeats, not the time). A new app process does not repeat them: after a restore
# the app is stopped and started again, which reopens Brain from its storage and reconnects in about 8 s. The guest's
# clock is also set to the host's, or its timestamps age past the daemon's window (adb root restarts adbd; the restored
# phone answers a moment later).
after_restore() {
  local guest ok=""
  for _ in $(seq 1 15); do
    adb -s "$serial" root > /dev/null 2>&1 || true
    adb -s "$serial" wait-for-device > /dev/null 2>&1 || true
    adb -s "$serial" shell date -u "$(date -u +%m%d%H%M%Y.%S)" > /dev/null 2>&1 || true
    guest=$(adb -s "$serial" shell date +%s 2>/dev/null | tr -d '\r')
    if [[ "$guest" =~ ^[0-9]+$ ]] && (( guest - $(date +%s) < 5 && $(date +%s) - guest < 5 )); then ok=1; break; fi
    sleep 1
  done
  [[ -n "$ok" ]] || die "could not set the guest clock to the host's"
  # Back to the shell user: a root caller's `am start --grant-read-uri-permission` grants nothing (shares are then refused).
  adb -s "$serial" unroot > /dev/null 2>&1 || true
  sleep 1; adb -s "$serial" wait-for-device > /dev/null 2>&1 || true
  # Right after a restore the guest's network drops connections to the host for a second or two (nc to 10.0.2.2 fails
  # now and then, then holds). The Dev app asks the dev server once at launch: when that one try meets a drop it runs its
  # built-in 16 MB bundle for ~135 s (then "cannot read asset ... MapleMono" in the daemon's app_crash log) and Brain
  # shows 2+ minutes late. So launch it only once the guest has reached the dev server 5 times in a row.
  adb -s "$serial" shell "timeout 60 sh -c 'n=0; while [ \$n -lt 5 ]; do if timeout 2 nc -z -w 1 10.0.2.2 $dev_port 2>/dev/null; then n=\$((n+1)); else n=0; fi; done'" \
    || die "the guest cannot reach the dev server at 10.0.2.2:$dev_port (is the dev server up?)"
  adb -s "$serial" shell am force-stop "$pkg"
  adb -s "$serial" shell monkey -p "$pkg" -c android.intent.category.LAUNCHER 1 > /dev/null 2>&1
  sleep 4 # the screen still shows the old frame for a moment
}

# The app says it is connected by the composer's placeholder on Brain ("Tell Brain"); offline it says "<computer> is
# offline" with a Try again cell, which is tapped. (The daemon's last_seen_at is not updated per connection.)
wait_connected() {
  local xml
  for _ in $(seq 1 40); do
    xml="$(screen)"
    if grep -q 'Tell Brain' <<< "$xml" && ! grep -q 'is offline' <<< "$xml"; then sleep 1; return 0; fi
    grep -q 'is offline' <<< "$xml" && tap 'wait-reconnect' 2 || true
    sleep 2
  done
  die "the app did not show a connected Brain within 80 s"
}

# The terminal corpus on the lane phone: each screen replayed into the real TerminalView (the debug build's
# TerminalReplayActivity, no daemon, no page), one PNG per screen cropped to the terminal. The caller checks the pictures
# (scripts/terminal-corpus/check.py); a screen with no picture here is a failure, not a skip.
corpus() {
  local out names=() n cols rows rect failed=0 dir="$repo/scripts/terminal-corpus"
  out=$(mkdir -p "${1:?corpus OUT [NAME..]}" && cd "$1" && pwd); shift
  names=("$@")
  [[ ${#names[@]} -gt 0 ]] || mapfile -t names < <(python3 -I -c "import json,sys;print('\n'.join(json.load(open(sys.argv[1]))))" "$dir/corpus.json")
  adb -s "$serial" shell settings put global window_animation_scale 0
  adb -s "$serial" shell settings put global transition_animation_scale 0
  adb -s "$serial" shell run-as "$pkg" mkdir -p files/corpus
  for n in "${names[@]}"; do
    adb -s "$serial" shell "run-as $pkg sh -c 'cat > files/corpus/$n.vt'" < "$dir/$n.vt"
    read -r cols rows < <(python3 -I -c "import json,sys;m=json.load(open(sys.argv[1]));print(m[sys.argv[2]]['cols'],m[sys.argv[2]]['rows'])" "$dir/corpus.json" "$n")
    adb -s "$serial" logcat -c
    adb -s "$serial" shell am start -S -W -n "$pkg/com.daoleno.mewla.terminal.TerminalReplayActivity" \
      --es file "corpus/$n.vt" --ei cols "$cols" --ei rows "$rows" ${LIGHT:+--ez light true} ${FONT:+--ef font "$FONT"} > /dev/null
    rect=""
    for _ in $(seq 1 40); do
      rect=$(adb -s "$serial" logcat -d -s TerminalReplay:I | sed -n 's/.*rect \([0-9]*\) \([0-9]*\) \([0-9]*\) \([0-9]*\).*/\3x\4+\1+\2/p' | tail -1)
      [[ -n "$rect" ]] && break
      sleep 0.5
    done
    if [[ -z "$rect" ]]; then echo "FAIL $n: the replay never reported its rect" >&2; failed=1; continue; fi
    sleep 0.5
    adb -s "$serial" exec-out screencap -p > "$out/full-$n.png"
    magick "$out/full-$n.png" -crop "$rect" +repage "$out/$n.png" && rm "$out/full-$n.png"
    echo "$n $rect"
  done
  return $failed
}

# flows/NAME.sh: a journey as a file (the phone back at the snapshot, then the file; its exit status is the verdict).
flows() {
  local names=("$@") f name start failed=0
  [[ "${names[0]:-all}" != all ]] || names=($(cd "$here/flows" && ls *.sh | sed 's/\.sh$//'))
  for name in "${names[@]}"; do
    f="$here/flows/$name.sh"; [[ -f "$f" ]] || die "no flow $name (flows/*.sh)"
    verb reset > /dev/null 2>&1 || { echo "FAIL $name (reset)"; failed=1; continue; }
    start=$(date +%s)
    if PHONE="$here/phone.sh" bash "$f" > "$lane/flow-$name.log" 2>&1; then echo "PASS $name $(( $(date +%s) - start )) s"
    else echo "FAIL $name $(( $(date +%s) - start )) s (see $lane/flow-$name.log)"; failed=1; fi
  done
  return $failed
}

verb() {
  local v="$1"; shift
  if [[ -n "$external" ]]; then
    case "$v" in reset|wait-connected|shot|tap|scroll|shows|dump|type|flow|share-file|share-text|open-with) ;; *) die "$v is not available with PHONE_EXTERNAL (the device, app and daemon are not this script's)" ;; esac
  fi
  case "$v" in
    up) emu_up; daemon_up; dev_up
      if [[ -z "${COLD:-}" ]]; then after_restore; wait_connected; fi ;;
    reset)
      # The plain path: the app's process is stopped and started again (Brain, connected). The phone is not restored.
      [[ -n "$external" ]] || { emu_up; daemon_up; dev_up; }
      adb -s "$serial" shell am force-stop "$pkg"
      adb -s "$serial" shell monkey -p "$pkg" -c android.intent.category.LAUNCHER 1 > /dev/null 2>&1
      wait_connected
      ;;
    restore)
      emu_up; daemon_up
      adb -s "$serial" emu avd snapshot load "$snapshot" | tail -1 >&2
      for _ in $(seq 1 30); do boot_done && break; sleep 1; done
      dev_down; dev_up
      after_restore
      wait_connected
      ;;
    wait-connected) wait_connected ;;
    shot) shot "$@" ;;
    tap) tap "$@" || die "no node matches $1" ;;
    shows) shows "$@" ;;
    # One swipe up through the middle of the screen: a row below the fold (the release replay's emulator is 320x640) comes in.
    scroll) read -r w h <<< "$(adb -s "$serial" shell wm size | grep -o '[0-9]*x[0-9]*' | tail -1 | tr x ' ')"
      adb -s "$serial" shell input swipe $((w / 2)) $((h * 7 / 10)) $((w / 2)) $((h * 3 / 10)) 300 ;;
    dump) dump ;;
    type) tap '^Tell Brain$' > /dev/null || die "no composer on the screen"; adb -s "$serial" shell input text "${1// /%s}" ;;
    flow) flows "$@" ;;
    corpus) corpus "$@" ;;
    share-file) share_file "$@" ;;
    share-text) share_text "$@" ;;
    open-with) open_with "$@" ;;
    install) install_app; emu_up; install_on_device ;;
    daemon-build) daemon_build ;;
    save) adb -s "$serial" emu avd snapshot save "$snapshot" | tail -1 >&2; du -sh "$HOME/.android/avd/$avd.avd/snapshots/$snapshot" >&2 ;;
    dev-link) point_at_dev ;;
    pair) adb -s "$serial" shell am start -n "$pkg/com.daoleno.mewla.MainActivity" -a android.intent.action.VIEW -d "'$(pair_link_for 10.0.2.2)'" > /dev/null ;;
    daemon-up) daemon_up ;;
    daemon-down) daemon_down ;;
    pair-link) pair_link_for "${1:-127.0.0.1}" ;;
    status)
      alive "$lane/emulator.pid" && echo "emulator: up (pid $(cat "$lane/emulator.pid"), $serial)" || echo "emulator: down"
      alive "$lane/daemon.pid" && echo "daemon: up (127.0.0.1:$daemon_port)" || echo "daemon: down"
      alive "$lane/dev.pid" && echo "dev server: up (:$dev_port)" || echo "dev server: down"
      [[ -d "$HOME/.android/avd/$avd.avd/snapshots/$snapshot" ]] && echo "snapshot $snapshot: present" || echo "snapshot $snapshot: missing (phone.sh setup)"
      ;;
    down) emu_down; daemon_down; dev_down ;;
    *) die "unknown verb $v (see the header of $0)" ;;
  esac
}

[[ $# -gt 0 ]] || { sed -n 2,36p "$0"; exit 0; }
if [[ "$1" == run ]]; then
  shift
  trap 'verb down' EXIT
  verb up
  args=()
  for a in "$@" "--"; do
    if [[ "$a" == "--" ]]; then
      ((${#args[@]} == 0)) || verb "${args[@]}"
      args=()
    else
      args+=("$a")
    fi
  done
else
  verb "$@"
fi
