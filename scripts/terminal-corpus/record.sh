#!/usr/bin/env bash
# Record a real program's output for the render corpus: the exact bytes it writes to its
# terminal, at a fixed size, replayable into any terminal of that size.
#
#   record.sh start <socket> <name> <cols> <rows> <command...>   # starts it in a private tmux
#   tmux -S <socket> send-keys -t <name> ...                      # drive it by hand
#   record.sh stop <socket> <name> [what]                         # stops the capture, adds corpus.json
#
# TERM is xterm-256color (COLORTERM=truecolor) inside, so programs write what they would to a
# phone terminal. The socket is a path in your scratch, never the default tmux server.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
cmd=${1:?start or stop}; sock=${2:?socket}; name=${3:?name}
case "$cmd" in
  start)
    cols=${4:?cols}; rows=${5:?rows}; shift 5
    : > "$here/$name.vt"
    tmux -S "$sock" new-session -d -s "$name" -x "$cols" -y "$rows" \
      -e TERM=xterm-256color -e COLORTERM=truecolor "sleep 0.5; exec $*"
    tmux -S "$sock" set -t "$name" status off
    tmux -S "$sock" resize-window -t "$name" -x "$cols" -y "$rows"
    tmux -S "$sock" pipe-pane -O -t "$name" "cat >> '$here/$name.vt'"
    printf '%s %s\n' "$cols" "$rows" > "$here/.$name.size"
    ;;
  stop)
    what=${4:-recorded}
    tmux -S "$sock" pipe-pane -t "$name"
    read -r cols rows < "$here/.$name.size"
    rm -f "$here/.$name.size"
    python3 -I - "$here/corpus.json" "$name" "$cols" "$rows" "$what" <<'PY'
import json, sys
path, name, cols, rows, what = sys.argv[1:]
m = json.load(open(path))
m[name] = {"cols": int(cols), "rows": int(rows), "what": what, "source": "record.sh"}
with open(path, "w") as f:
    json.dump(dict(sorted(m.items())), f, indent=2, ensure_ascii=False)
    f.write("\n")
PY
    ;;
  *) echo "start or stop" >&2; exit 1 ;;
esac
