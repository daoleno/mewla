#!/usr/bin/env bash
# From Brain, the cat opens the computer page; Calendar opens from there.
set -euo pipefail
"$PHONE" tap 'home-cat'
"$PHONE" tap 'Calendar'
"$PHONE" shows 'calendar-page'
