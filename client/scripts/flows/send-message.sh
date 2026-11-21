#!/usr/bin/env bash
# Send a message to Brain from the composer and see its bubble. The text is new each run: the test daemon keeps its
# messages across a reset (only the phone goes back to the snapshot).
set -euo pipefail
msg="lane$RANDOM"
"$PHONE" type "$msg"
"$PHONE" tap '^send$'
"$PHONE" shows "$msg"
