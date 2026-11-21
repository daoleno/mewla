#!/usr/bin/env bash
# A picture shared from another app lands in Brain's composer as a chip, not as "Cannot read this file".
set -euo pipefail
name="harbor$RANDOM.png"
dir="$(mktemp -d "${TMPDIR:-/tmp}/share-in.XXXXXX")"
python3 -I -c 'import base64,sys;open(sys.argv[1],"wb").write(base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="))' "$dir/$name"
"$PHONE" share-file "$dir/$name"
"$PHONE" shows "$name"
! "$PHONE" shows 'Cannot read' 1
