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
