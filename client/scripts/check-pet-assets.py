#!/usr/bin/env python3
"""Every pet image the app names is in a platform's build.

The app names its pet images in client/ui/pets/assets.ts (one import per clip, still, portrait
and empty bed; core/pets/assets.test.ts ties that table to the pets manifest). This checks that
each one is in the packaged app, where that platform's runtime looks for it:

  check-pet-assets.py web <dir>          the `nsh package --target web` folder (index.html beside it):
                                         index.html's asset map names the file and the file is there
                                         (or its .gz, once build-web-ui.sh compressed it)
  check-pet-assets.py android <apk>      the APK holds it under assets/
  check-pet-assets.py ios <Mewla.app>    the app bundle holds it

Exits 1 and lists what is missing.
"""

import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TABLE = ROOT / "ui" / "pets" / "assets.ts"


def named() -> list[str]:
    """The pet images assets.ts imports, as asset paths ("assets/pets/p05/waking.webp")."""
    paths = re.findall(r'from "\.\./(assets/pets/[^"]+)"', TABLE.read_text())
    if not paths:
        sys.exit(f"no pet images found in {TABLE}")
    return sorted(set(paths))


def web(folder: Path, wanted: list[str]) -> list[str]:
    html = (folder / "index.html").read_text()
    # The asset map: "assets/pets/p05/waking.webp":"assets/assets/pets/p05/waking.cbc7fa95db.webp".
    mapped = dict(re.findall(r'"(assets/pets/[^"]+)":"([^"]+)"', html))
    missing = []
    for path in wanted:
        target = mapped.get(path)
        if target is None:
            missing.append(f"{path}: not in index.html's asset map")
        elif not (folder / target).is_file() and not (folder / (target + ".gz")).is_file():
            missing.append(f"{path}: mapped to {target}, which is not in the package")
    return missing


def held(entries: set[str], wanted: list[str]) -> list[str]:
    return [f"{path}: not in the build" for path in wanted if not any(entry.endswith("/" + path) for entry in entries)]


def android(apk: Path, wanted: list[str]) -> list[str]:
    listing = subprocess.run(["unzip", "-Z1", str(apk)], check=True, capture_output=True, text=True).stdout
    return held({line for line in listing.splitlines() if line.startswith("assets/")}, wanted)


def ios(app: Path, wanted: list[str]) -> list[str]:
    return held({"/" + str(path.relative_to(app)) for path in app.rglob("*") if path.is_file()}, wanted)


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[1] not in ("web", "android", "ios"):
        sys.exit(__doc__)
    platform, where = sys.argv[1], Path(sys.argv[2])
    wanted = named()
    missing = {"web": web, "android": android, "ios": ios}[platform](where, wanted)
    if missing:
        print(f"{len(missing)} of {len(wanted)} pet images are missing from the {platform} build ({where}):", file=sys.stderr)
        for line in missing:
            print(f"  {line}", file=sys.stderr)
        sys.exit(1)
    print(json.dumps({"platform": platform, "pet_images": len(wanted), "missing": 0}))


if __name__ == "__main__":
    main()
