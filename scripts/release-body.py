#!/usr/bin/env python3
"""Write a release's notes at release time, from its commits: nothing about them is committed.

  release-body.py --tag vX.Y.Z [--previous vA.B.C] > notes.md

The changes are the subjects of the commits since the previous release tag (merges and release
preparation commits left out), newest first; then how to get it and how to verify the downloads,
with the Android certificate fingerprint scripts/verify-release-identity.sh pins. --previous
defaults to the newest release tag that is an ancestor of --tag. A model may polish the text later
on its own key; these notes never wait for one.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY_URL = "https://github.com/daoleno/mewla"
INSTALL_SCRIPT_URL = "https://mewla.ai/install.sh"
INSTALL_DOCS_URL = "https://mewla.ai/docs/install/"
TAG = re.compile(r"^v\d+\.\d+\.\d+(-beta\.\d+)?$")
PREPARE = re.compile(r"^Prepare v\d+\.\d+\.\d+(-beta\.\d+)?$")


def git(*args: str) -> str:
    return subprocess.run(["git", "-C", str(ROOT), *args], check=True, capture_output=True, text=True).stdout


def previous_tag(tag: str) -> str:
    for line in git("tag", "--merged", f"{tag}^", "--sort=-v:refname", "v*").splitlines():
        if TAG.match(line) and line != tag:
            return line
    raise SystemExit(f"error: no release tag before {tag}")


def certificate() -> str:
    source = (ROOT / "scripts/verify-release-identity.sh").read_text(encoding="utf-8")
    match = re.search(r'^EXPECTED_CERT_FP="([0-9A-F]{2}(?::[0-9A-F]{2}){31})"$', source, re.MULTILINE)
    if not match:
        raise SystemExit("error: no certificate fingerprint in scripts/verify-release-identity.sh")
    return match.group(1)


def changes(previous: str, tag: str) -> list[str]:
    subjects = git("log", "--no-merges", "--format=%s", f"{previous}..{tag}").splitlines()
    return [s.strip() for s in subjects if s.strip() and not PREPARE.match(s.strip())]


def body(tag: str, previous: str, subjects: list[str], cert: str) -> str:
    version = tag.removeprefix("v")
    listed = "\n".join(f"- {s}" for s in subjects) or "- Maintenance release."
    return f"""# Mewla {version}

## Changes

{listed}

## Get it

- **Already installed?** Run `mewla update` on your computer, and install the new APK on Android.
- **New here?** `curl -fsSL {INSTALL_SCRIPT_URL} | sh` picks the right file for you. See [Install]({INSTALL_DOCS_URL}).
- **Mac (Apple Silicon):** `mewla-darwin-arm64.tar.gz`
- **Linux x64:** `mewla-linux-amd64.tar.gz`
- **Linux ARM:** `mewla-linux-arm64.tar.gz`
- **Windows x64:** `mewla-windows-amd64.zip`, or in PowerShell: `irm https://mewla.ai/install.ps1 | iex`
- **Android:** `mewla-android-arm64-{tag}.apk`. Android asks you to allow installs from unknown sources.

All changes: [{previous}...{tag}]({REPOSITORY_URL}/compare/{previous}...{tag})

<details markdown="1">
<summary>Verify downloads</summary>

`mewla update` and the installer check downloads for you. To check one by hand against `SHA256SUMS`:

```sh
grep 'mewla-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -        # Linux
grep 'mewla-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c -   # Mac
```

On Windows, in PowerShell, compare with the `mewla-windows-amd64.zip` line of `SHA256SUMS`:

```powershell
(Get-FileHash mewla-windows-amd64.zip -Algorithm SHA256).Hash.ToLower()
```

Android signing certificate SHA-256:

```text
{cert}
```

</details>
"""


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--tag", required=True)
    parser.add_argument("--previous")
    args = parser.parse_args(argv)
    if not TAG.match(args.tag):
        raise SystemExit(f"error: not a release tag: {args.tag}")
    previous = args.previous or previous_tag(args.tag)
    sys.stdout.write(body(args.tag, previous, changes(previous, args.tag), certificate()))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
