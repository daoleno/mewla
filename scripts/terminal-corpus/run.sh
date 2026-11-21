#!/usr/bin/env bash
# The terminal render corpus as one command with a verdict: every screen of corpus.json drawn by the web client's
# terminal page (libghostty-vt as WebAssembly on a canvas, the way the app runs it) in each browser engine and
# checked for what the renderer got wrong before (rows of block and line glyphs that leave seams, emoji drawn in the
# wrong cell, a screen that never drew). Prints PASS or FAIL per engine and exits 1 on any failure.
#
#   scripts/terminal-corpus/run.sh [--engines "chromium firefox webkit"] [--android] [--out DIR]
#
#   --engines  default: Chromium and Firefox, plus WebKit on a CI runner (WebKit needs Ubuntu's libraries; not
#              installed on every dev machine, so it is asked for by name here and never silently skipped);
#              "none" for the Android leg alone
#   --android  also the Android leg: the same screens in the Dev app's TerminalView on the lane emulator
#              (client/scripts/phone.sh run corpus: boots mewla-lane under its lock and 8 GiB memory check, about 70 s;
#              never in CI). Needs the debug build with TerminalReplayActivity: `client/scripts/phone.sh install` after a
#              Kotlin change in the terminal.
#              CORPUS_ADVISORY="firefox": engines whose failure is a warning, not a failure (CI sets it for Firefox)
#   --out DIR  keep the pictures there (default: a fresh directory under $TMPDIR, printed)
#
# The Playwright version is pinned in playwright-version.txt (its Chromium, Firefox and WebKit builds are what the
# corpus is judged on); playwright-core and the browsers are kept in ~/.cache/mewla/terminal-corpus (CORPUS_CACHE),
# installed on first use, and never in the repository. Pillow for the pixel checks comes from the system Python,
# else uv, else a venv in the cache. iOS draws the same corpus in client.yml's TerminalTests.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
cache="${CORPUS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/mewla/terminal-corpus}"
version="$(tr -d '[:space:]' < "$here/playwright-version.txt")"

engines=""
android=""
out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --engines) engines="${2:?--engines needs a list}"; shift 2 ;;
    --android) android=1; shift ;;
    --out) out="${2:?--out needs a directory}"; shift 2 ;;
    -h|--help) sed -n '2,/^set -euo/p' "$0" | sed '$d'; exit 0 ;;
    *) echo "run.sh: unknown argument $1" >&2; exit 2 ;;
  esac
done
if [[ -z "$engines" ]]; then
  engines="chromium firefox"
  [[ -z "${GITHUB_ACTIONS:-}" ]] || engines="chromium firefox webkit"
fi
[[ "$engines" != none ]] || engines=""
[[ -n "$out" ]] || out="$(mktemp -d "${TMPDIR:-/tmp}/terminal-corpus.XXXXXX")"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
mkdir -p "$cache"

log() { echo "[corpus] $*" >&2; }

# ---- tools --------------------------------------------------------------------------------------------------
command -v bun > /dev/null || { echo "run.sh: bun is not installed" >&2; exit 1; }

if [[ "$(cat "$cache/node_modules/playwright-core/package.json" 2> /dev/null | python3 -I -c 'import json,sys;print(json.load(sys.stdin).get("version",""))' 2> /dev/null || true)" != "$version" ]]; then
  log "installing playwright-core $version into $cache"
  [[ -f "$cache/package.json" ]] || echo '{"private":true}' > "$cache/package.json"
  (cd "$cache" && timeout 300 bun add "playwright-core@$version" > /dev/null)
fi
core="$cache/node_modules/playwright-core"
export PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$cache/browsers}"

pillow_python=()
if python3 -I -c 'import PIL' 2> /dev/null; then
  pillow_python=(python3 -I)
elif command -v uv > /dev/null; then
  pillow_python=(uv run -q --no-project --with pillow python -I)
else
  [[ -x "$cache/venv/bin/python" ]] || { python3 -m venv "$cache/venv" && "$cache/venv/bin/pip" install -q pillow; }
  pillow_python=("$cache/venv/bin/python" -I)
fi

# A browser missing from the cache is installed (a runner also gets the libraries it needs; elsewhere that is the
# machine's own business). Never longer than 10 minutes: a stuck download is a failure, not a hang.
install_browser() {
  local engine="$1" args=()
  # On a runner the libraries (apt) come with every run: a restored browser cache holds the browsers, never the libraries
  # (WebKit then fails with "libevent-2.1.so.7: cannot open shared object file"); the download itself is skipped when present.
  if [[ -z "${GITHUB_ACTIONS:-}" ]]; then
    compgen -G "$PLAYWRIGHT_BROWSERS_PATH/$engine-*/INSTALLATION_COMPLETE" > /dev/null && return 0
  else
    args+=(--with-deps)
  fi
  log "installing $engine into $PLAYWRIGHT_BROWSERS_PATH"
  timeout 600 bun "$core/cli.js" install "${args[@]}" "$engine"
}

# ---- the web engines ----------------------------------------------------------------------------------------
declare -a verdicts=()
failed=0
for engine in $engines; do
  dir="$out/$engine"
  mkdir -p "$dir"
  started=$SECONDS
  verdict=""
  if ! install_browser "$engine" > "$dir/install.log" 2>&1; then
    verdict="FAIL $engine: the browser could not be installed (see $dir/install.log)"
  else
    # BROWSER and CHROME name one binary: left over from another job they would launch the wrong engine.
    # One more try for a run that died in the browser itself: Firefox on a CI runner once ended in Playwright's "Serialized
    # error must have either an error or a value" and passed on the rerun (11-21). A renderer fault fails both tries.
    set +e
    for attempt in 1 2; do
      env -u BROWSER -u CHROME ENGINE="$engine" PLAYWRIGHT_CORE="$core" timeout 600 bun "$here/web.ts" "$dir" > "$dir/web.log" 2>&1
      code=$?
      grep -q "^corpus web $engine: ok " "$dir/web.log" && break
      [[ $attempt == 1 ]] && { log "$engine failed ($(grep -m1 -E '^error:|^corpus web .*FAIL' "$dir/web.log" || echo "exit $code")); trying once more"; cp "$dir/web.log" "$dir/web-attempt1.log"; tail -12 "$dir/web.log" | grep -v '^[0-9]* |' >&2; }
    done
    set -e
    # bun can crash as it exits after a Playwright run: the verdict is web.ts's own line, not the exit code alone.
    if ! grep -q "^corpus web $engine: ok " "$dir/web.log"; then
      verdict="FAIL $engine: $(grep -m1 -E '^error:|^corpus web .*FAIL' "$dir/web.log" || grep -m1 Error "$dir/web.log" || echo "no verdict line, exit $code") (see $dir/web.log)"
    elif ! "${pillow_python[@]}" "$here/check.py" "$dir" > "$dir/check.log" 2>&1; then
      verdict="FAIL $engine: $(grep -m1 '^FAIL' "$dir/check.log" || tail -1 "$dir/check.log") (see $dir/check.log)"
    else
      verdict="PASS $engine: $(tail -1 "$dir/check.log")"
    fi
  fi
  if [[ "$verdict" != PASS* ]]; then
    # An advisory engine (CORPUS_ADVISORY, CI's Firefox: see README.md) is reported as a warning, never as the job's failure.
    if [[ " ${CORPUS_ADVISORY:-} " == *" $engine "* ]]; then
      verdict="WARN (advisory, not failing) ${verdict#FAIL }"
      echo "::warning title=Terminal corpus: $engine::$verdict" >&2
    else
      failed=1
    fi
    # On a runner the scratch directory is gone with the job: what failed is printed here, not only named.
    for f in "$dir/install.log" "$dir/web-attempt1.log" "$dir/web.log" "$dir/check.log"; do
      [[ -s "$f" ]] && { echo "::group::$engine: $(basename "$f")"; tail -60 "$f"; echo "::endgroup::"; } >&2
    done
  fi
  verdicts+=("$verdict, $((SECONDS - started)) s")
  log "${verdicts[-1]}"
done

# ---- Android: the same screens in the Dev app's TerminalView on the lane emulator ---------------------------------
if [[ -n "$android" ]]; then
  dir="$out/android"
  mkdir -p "$dir"
  started=$SECONDS
  if "$root/client/scripts/phone.sh" run corpus "$dir" > "$dir/phone.log" 2>&1 \
     && "${pillow_python[@]}" "$here/check.py" "$dir" > "$dir/check.log" 2>&1; then
    verdicts+=("PASS android: $(tail -1 "$dir/check.log"), $((SECONDS - started)) s")
  else
    failed=1
    verdicts+=("FAIL android: $(grep -m1 -E '^FAIL' "$dir/check.log" "$dir/phone.log" 2> /dev/null | head -1 || echo "see $dir/phone.log"), $((SECONDS - started)) s")
  fi
  log "${verdicts[-1]}"
fi

echo
[[ ${#verdicts[@]} -gt 0 ]] || { echo "run.sh: nothing to run (no engines, no --android)" >&2; exit 2; }
printf '%s\n' "${verdicts[@]}"
if [[ $failed -ne 0 ]]; then
  echo "terminal corpus: FAIL (pictures in $out)"
  exit 1
fi
echo "terminal corpus: PASS (pictures in $out)"
