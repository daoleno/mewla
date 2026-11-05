#!/usr/bin/env bash
# Builds the Chrome Web Store package of Mewla for Chrome: the extension's files
# without the manifest "key" (the store assigns the item its own ID; a key in
# the upload is not accepted for an item the store signs).
#   scripts/build-chrome-extension.sh <output.zip>
set -euo pipefail
out=${1:?usage: build-chrome-extension.sh <output.zip>}
src="$(cd "$(dirname "$0")/.." && pwd)/daemon/agentuse/extension"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp -R "$src"/. "$work"/
python3 - "$work/manifest.json" <<'PY'
import json, sys
path = sys.argv[1]
m = json.load(open(path))
m.pop("key", None)
json.dump(m, open(path, "w"), indent=2)
open(path, "a").write("\n")
PY
rm -f "$out"
python3 - "$work" "$out" <<'PY'
import os, sys, zipfile
root, out = sys.argv[1:3]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for base, _, files in sorted(os.walk(root)):
        for name in sorted(files):
            if name == ".DS_Store":
                continue
            path = os.path.join(base, name)
            info = zipfile.ZipInfo(os.path.relpath(path, root), (2026, 1, 1, 0, 0, 0))
            info.external_attr = 0o644 << 16
            z.writestr(info, open(path, "rb").read(), zipfile.ZIP_DEFLATED)
    print("\n".join(f"{i.file_size:8d}  {i.filename}" for i in z.infolist()))
PY
