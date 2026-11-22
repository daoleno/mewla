#!/usr/bin/env bash
# A stand-in `claude` for the Mac smoke run on a CI runner (macos-app.yml), which has no agent: it answers
# `--version`, then draws what Mewla reads as Claude Code ready for input (the header, an empty ❯ composer and the
# permission footer). It calls no model and writes nothing to disk.
case "${1:-}" in
  --version | -v) echo "2.1.0 (Claude Code)"; exit 0 ;;
  auth) echo '{"loggedIn": true}'; exit 0 ;;
esac
draw() {
  printf '\033[H\033[2JClaude Code v2.1.0\r\n\r\n'
  printf '────────────────────────────────────────\r\n❯ \r\n'
  printf '────────────────────────────────────────\r\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\r\n'
}
# A real TUI owns its screen and does not echo the startup brief through the terminal driver.
stty -echo
printf '\033[?1049h'
trap 'printf "\033[?1049l"; stty echo' EXIT
draw
while IFS= read -r line; do
  draw
done
