# The booted emulator's screen as text, for the release gate's scripts (release-launch.sh, release-flows.sh): `source` it.
# `tap NODE [SECS]` taps the node, `shows NODE [SECS]` waits for it; NODE is a text, content-desc or resource-id (exact match
# first, the last such node, else one that contains it, case-insensitive), or "re:<pattern>". Uses adb's one device.
screen() {
  # A failed dump leaves the previous file: remove it first so a stale screen is never read.
  adb shell rm -f /sdcard/ui.xml
  adb shell uiautomator dump /sdcard/ui.xml > /dev/null 2>&1 || true
  adb exec-out cat /sdcard/ui.xml 2>/dev/null || true
}
# The centre of the last node whose text, content-desc or resource-id is exactly $1 (covered pages of a navigation stack come
# first in the dump), else of the last one that contains it, case-insensitive; "re:<pattern>" matches the pattern instead.
find_node() {
  python3 -I -c '
import re, sys
xml, want = sys.stdin.read(), sys.argv[1]
exact, partial = None, None
for node in re.finditer(r"<node [^>]*>", xml):
    n = node.group(0)
    values = [v for k, v in re.findall(r"\b(text|content-desc|resource-id)=\"([^\"]*)\"", n)]
    box = re.search(r"bounds=\"\[(\d+),(\d+)\]\[(\d+),(\d+)\]\"", n)
    if not box:
        continue
    x1, y1, x2, y2 = map(int, box.groups())
    at = f"{(x1 + x2) // 2} {(y1 + y2) // 2}"
    if want.startswith("re:"):
        if any(re.search(want[3:], v) for v in values):
            exact = at
    elif want in values:
        exact = at
    elif any(want.lower() in v.lower() for v in values):
        partial = at
if exact or partial:
    print(exact or partial)
' "$1"
}
tap() {
  local deadline=$((SECONDS + ${2:-20})) at
  while ((SECONDS < deadline)); do
    at=$(screen | find_node "$1")
    if [[ -n "$at" ]]; then adb shell input tap $at; return 0; fi
    sleep 1
  done
  return 1
}
shows() {
  local deadline=$((SECONDS + ${2:-1}))
  while ((SECONDS < deadline)); do
    [[ -n "$(screen | find_node "$1")" ]] && return 0
    sleep 1
  done
  return 1
}
