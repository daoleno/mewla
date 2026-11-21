#!/usr/bin/env bash
# Whether the commits CI is checking touch what the web corpus covers (paths.txt): prints `run=true` or `run=false`
# and, on a runner, appends it to $GITHUB_OUTPUT. The base is the event's: a push's `before`, a pull request's base
# commit (read from $GITHUB_EVENT_PATH, so the checkout stays shallow). With no event, no base or a base that can't
# be fetched, it says run=true: when in doubt the corpus runs.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
base=""
if [[ -n "${GITHUB_EVENT_PATH:-}" && -f "$GITHUB_EVENT_PATH" ]]; then
  base=$(python3 -I -c '
import json, sys
e = json.load(open(sys.argv[1]))
print((e.get("pull_request") or {}).get("base", {}).get("sha") or e.get("before") or "")' "$GITHUB_EVENT_PATH")
fi
verdict=true
if [[ -n "$base" && "$base" != 0000000000000000000000000000000000000000 ]]; then
  if git cat-file -e "$base^{commit}" 2>/dev/null || git fetch --quiet --depth=1 origin "$base" 2>/dev/null; then
    mapfile -t changed < <(git diff --name-only "$base" HEAD)
    mapfile -t patterns < <(grep -v '^#' scripts/terminal-corpus/paths.txt | grep -v '^$')
    verdict=false
    for path in "${changed[@]}"; do
      for pattern in "${patterns[@]}"; do
        # shellcheck disable=SC2053
        if [[ "$path" == $pattern ]]; then verdict=true; echo "terminal corpus: $path changed" >&2; break 2; fi
      done
    done
    [[ "$verdict" == true ]] || echo "terminal corpus: none of ${#changed[@]} changed paths is the terminal's; not run" >&2
  else
    echo "terminal corpus: base $base is not available; running it" >&2
  fi
else
  echo "terminal corpus: no base commit to compare with; running it" >&2
fi
echo "run=$verdict"
[[ -z "${GITHUB_OUTPUT:-}" ]] || echo "run=$verdict" >> "$GITHUB_OUTPUT"
