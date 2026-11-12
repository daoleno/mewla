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
# same library: concealed text stays hidden (scripts/tests/terminal_vt_conceal.cpp), and the render
# corpus (scripts/terminal-corpus) comes through cell by cell with every attribute and the cursor
# (scripts/tests/terminal_corpus_cells.cpp). The terminal's own glyphs join their neighbours
# (scripts/tests/terminal_glyphs.cpp, no library).
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT
NATIVE="$ROOT/client/native/terminal"
for t in terminal_vt_conceal terminal_corpus_cells; do
  "${CXX:-c++}" -std=c++17 -w -D_Nullable= -D_Nonnull= -I"$NATIVE" -I"$CACHE/include" \
    "$NATIVE/TerminalVt.cpp" "$ROOT/scripts/tests/$t.cpp" "$CACHE/$TARGET/libghostty-vt.a" -lpthread -lm -o "$OUT/$t"
done
"$OUT/terminal_vt_conceal"
"$OUT/terminal_corpus_cells" "$ROOT/scripts/terminal-corpus"
"${CXX:-c++}" -std=c++17 -Wall -Werror -I"$NATIVE" "$NATIVE/TerminalGlyphs.cpp" "$ROOT/scripts/tests/terminal_glyphs.cpp" -o "$OUT/terminal_glyphs"
"$OUT/terminal_glyphs"
