#!/usr/bin/env bash
# Test the session host's terminal engine (daemon/ghosttyvt), the host on top
# of it (daemon/sessionhost) and the watcher's host backend with libghostty-vt
# linked in.
#
# The static libraries are built into a cache outside the checkout, never into
# daemon/ghostty-libs: that directory switches mewla-dev to the tagged build.
# Extra arguments go to go test.
#
#   ./scripts/test-ghosttyvt.sh
#   ./scripts/test-ghosttyvt.sh -race -run Snapshot
#
# Cache: ${MEWLA_GHOSTTY_DESKTOP_CACHE:-$HOME/.cache/mewla/ghostty-desktop}.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${MEWLA_GHOSTTY_DESKTOP_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/mewla/ghostty-desktop}"

cd "$ROOT/daemon"
TARGET="$(go env GOOS)_$(go env GOARCH)"
"$ROOT/scripts/build-libghostty-desktop.sh" --out "$CACHE" --targets "$TARGET"
export CGO_ENABLED=1 CGO_CFLAGS="-I$CACHE/include" CGO_LDFLAGS="-L$CACHE/$TARGET"
go test -tags ghosttyvt -count=1 "$@" ./ghosttyvt/... ./sessionhost/...
# The watcher's session host backend against a real host; the rest of the
# watcher does not use the engine and runs in the untagged suite.
go test -tags ghosttyvt -count=1 -run 'TestHost|TestSessionHost' "$@" ./watcher/
go test -tags ghosttyvt -count=1 -run 'TestHostTerminal' "$@" ./terminal/
# The app's terminal cell packer (client/native/terminal/TerminalVt.cpp, Android's and iOS's) on the
# same library: concealed text stays hidden (scripts/tests/terminal_vt_conceal.cpp), soft-wrapped rows
# are flagged and pastes framed per bracketed-paste mode (terminal_vt_select.cpp), and the render
# corpus (scripts/terminal-corpus) comes through cell by cell with every attribute and the cursor
# (scripts/tests/terminal_corpus_cells.cpp), and keys, the mouse and focus encode for the program's
# modes (terminal_vt_input.cpp). The terminal's own glyphs join their neighbours
# (scripts/tests/terminal_glyphs.cpp, no library).
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT
NATIVE="$ROOT/client/native/terminal"
for t in terminal_vt_conceal terminal_vt_select terminal_vt_input terminal_corpus_cells; do
  "${CXX:-c++}" -std=c++17 -w -D_Nullable= -D_Nonnull= -I"$NATIVE" -I"$CACHE/include" \
    "$NATIVE/TerminalVt.cpp" "$ROOT/scripts/tests/$t.cpp" "$CACHE/$TARGET/libghostty-vt.a" -lpthread -lm -o "$OUT/$t"
done
"$OUT/terminal_vt_conceal"
"$OUT/terminal_vt_select"
"$OUT/terminal_vt_input"
"$OUT/terminal_corpus_cells" "$ROOT/scripts/terminal-corpus"
# The web page names libghostty-vt's keys by W3C code in the header's order (terminal.js KEY_NAMES).
python3 - "$CACHE/include/ghostty/vt/key/event.h" "$ROOT/client/ui/web/terminal/terminal.js" <<'PY'
import re, sys
header, page = open(sys.argv[1]).read(), open(sys.argv[2]).read()
header = header[header.index("GHOSTTY_KEY_UNIDENTIFIED = 0"):header.index("} GhosttyKey;")]
keys = [(n, int(v)) for n, v in re.findall(r"GHOSTTY_KEY_([A-Z0-9_]+)\s*=\s*(\d+),", header) if n != "UNIDENTIFIED"]
w3c = lambda n: "Key" + n if re.fullmatch(r"[A-Z]", n) else "".join(p if p.isdigit() else p[0] + p[1:].lower() for p in n.split("_"))
want = [w3c(n) for n, v in sorted(keys, key=lambda k: k[1])]
assert [v for _, v in sorted(keys, key=lambda k: k[1])] == list(range(1, len(keys) + 1)), "GhosttyKey is no longer 1..n"
got = " ".join(re.findall(r'"([^"]*)"\s*\+?', page[page.index("var KEY_NAMES = ("):page.index(').split(" ");')])).split()
if got != want:
    sys.exit(f"terminal.js KEY_NAMES differs from ghostty/vt/key/event.h: first difference at {next(i for i, (a, b) in enumerate(zip(got + [''], want + [''])) if a != b)}")
print(f"terminal.js: {len(got)} key names match libghostty-vt's")
PY
"${CXX:-c++}" -std=c++17 -Wall -Werror -I"$NATIVE" "$NATIVE/TerminalGlyphs.cpp" "$ROOT/scripts/tests/terminal_glyphs.cpp" -o "$OUT/terminal_glyphs"
"$OUT/terminal_glyphs"
