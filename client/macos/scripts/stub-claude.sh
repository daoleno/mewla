#!/usr/bin/env bash
# A stand-in `claude` for the Mac smoke run on a CI runner (macos-app.yml), which has no agent: it answers
# `--version`, then draws what Mewla reads as Claude Code ready for input (the header, an empty ❯ composer and the
# permission footer) and echoes each line it is sent above the composer. It calls no model and writes nothing.
case "${1:-}" in
  --version | -v) echo "2.1.0 (Claude Code)"; exit 0 ;;
  auth) echo '{"loggedIn": true}'; exit 0 ;;
esac
draw() {
  printf '\r\n\033[2m────────────────────────────────────────\033[0m\r\n❯ \r\n'
  printf '\033[2m  ⏵⏵ bypass permissions on (shift+tab to cycle)\033[0m\r\n'
}
printf '\033[1m Claude Code v2.1.0\033[0m  (stub on a CI runner)\r\n'
draw
# A clock in the top right corner each second (cursor saved and restored), so a terminal that attaches at any moment
# has fresh output to draw.
# It ends with the stub however the stub ends (a signal skips an EXIT trap).
stub=$$
(while kill -0 "$stub" 2>/dev/null; do printf '\0337\033[1;60H\033[2mstub %s\033[0m\0338' "$(date +%H:%M:%S)" || exit; sleep 1; done) &
trap 'kill $! 2>/dev/null; exit' EXIT HUP INT TERM
while IFS= read -r line; do
  printf '\r\n\033[1m> %s\033[0m\r\n  (stub: no model here)\r\n' "$line"
  draw
done
