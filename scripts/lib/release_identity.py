#!/usr/bin/env python3
"""Mewla's release identity: one version, and every build number derived from it.

The version is client/ui/nsh.json "version": X.Y.Z or X.Y.Z-beta.N. It is the
daemon's version, the Android versionName and (without -beta.N) the iOS
marketing version. Store build numbers follow from it by one rule:

    code = X * 1_000_000 + Y * 10_000 + Z * 100 + (N for beta.N, 99 for a release)
    with Y <= 99, Z <= 99 and 1 <= N <= 98

so they grow with SemVer precedence (0.3.2-beta.1 = 30201 < 0.3.2 = 30299 <
0.3.3-beta.1 = 30301) and start above every Expo build (Android versionCode
142 and iOS build 140 at 0.3.1). The Android versionCode and the iOS
CFBundleVersion are both this code, and a code names its version back
(version_of_code). client/android/app/build.gradle.kts computes the same in
Kotlin; the release verifiers compare what a build carries with this module,
so the two cannot drift unnoticed.

The Preview (TestFlight) app's marketing version (CFBundleShortVersionString)
is its train ("testflight_version"): the X.Y.Z of the first iOS release of a
minor, pinned in client/ios/project.yml MEWLA_TESTFLIGHT_VERSION and kept for
every later release of that minor. Beta App Review fully reviews only the first
build of a marketing version, so later releases of the train reach testers in
minutes, and a train named by a real release sorts above every earlier one
(TestFlight shows the highest marketing version as the newest). For 0.4.x the
train is 0.4.9: 0.4.10 is "0.4.9 (41099)" in TestFlight and iOS Settings; the
build number names the release (version_of_code: 41099 is 0.4.10).
scripts/prepare-release.py opens the next train on a minor bump. The app shows
its real version from the bundled client/ui/nsh.json and Info.plist
MewlaVersion (MEWLA_APP_VERSION). The production (App Store) identity keeps
X.Y.Z.

Usage:
  scripts/lib/release_identity.py                 # key=value lines (GITHUB_OUTPUT)
  scripts/lib/release_identity.py version_code    # one field
  scripts/lib/release_identity.py --json
  scripts/lib/release_identity.py --version 0.3.2-beta.1 version_code
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SOURCE = "client/ui/nsh.json"
IOS_PROJECT = "client/ios/project.yml"
TRAIN_RE = re.compile(r'(?m)^\s+MEWLA_TESTFLIGHT_VERSION: "([^"\n]*)"\s*$')
VERSION_RE = re.compile(
    r"^(?P<major>0|[1-9][0-9]*)\.(?P<minor>0|[1-9][0-9]*)\.(?P<patch>0|[1-9][0-9]*)"
    r"(?:-beta\.(?P<beta>[1-9][0-9]*))?$"
)
RELEASE_SUFFIX = 99
MAX_BETA = 98
# The shipped app's identity; the client must keep it (it upgrades the Expo app in place).
PACKAGE = "com.daoleno.mewla"
IOS_BUNDLE_IDS = {"production": "com.daoleno.mewla", "preview": "com.daoleno.mewla.preview"}


class IdentityError(ValueError):
    """The version source is missing or holds no valid release version."""


def parse(version: str) -> tuple[int, int, int, int | None]:
    match = VERSION_RE.fullmatch(version) if isinstance(version, str) else None
    if match is None:
        raise IdentityError(f"version must be X.Y.Z or X.Y.Z-beta.N; got {version!r}")
    major, minor, patch = (int(match.group(name)) for name in ("major", "minor", "patch"))
    beta = int(match.group("beta")) if match.group("beta") else None
    if minor > 99 or patch > 99:
        raise IdentityError(f"minor and patch must be at most 99 for a build number; got {version}")
    if beta is not None and beta > MAX_BETA:
        raise IdentityError(f"beta.N must be at most {MAX_BETA} for a build number; got {version}")
    return major, minor, patch, beta


def build_code(version: str) -> int:
    """The Android versionCode and iOS CFBundleVersion of a version."""
    major, minor, patch, beta = parse(version)
    return major * 1_000_000 + minor * 10_000 + patch * 100 + (RELEASE_SUFFIX if beta is None else beta)


def marketing_version(version: str) -> str:
    major, minor, patch, _ = parse(version)
    return f"{major}.{minor}.{patch}"


def version_of_code(code: int) -> str | None:
    """The version whose build code this is, or None for a number the rule never makes (Expo's)."""
    if not isinstance(code, int) or isinstance(code, bool) or code < 10_000:
        return None
    major, rest = divmod(code, 1_000_000)
    minor, rest = divmod(rest, 10_000)
    patch, suffix = divmod(rest, 100)
    if suffix == 0:
        return None
    version = f"{major}.{minor}.{patch}" + ("" if suffix == RELEASE_SUFFIX else f"-beta.{suffix}")
    return version if build_code(version) == code else None


def check_testflight_version(version: str, train: str) -> str:
    """The pinned train, if it can carry this release: an X.Y.Z of the same minor (X.Y)."""
    major, minor, _, _ = parse(version)
    match = VERSION_RE.fullmatch(train) if isinstance(train, str) else None
    if match is None or match.group("beta"):
        raise IdentityError(f"MEWLA_TESTFLIGHT_VERSION must be X.Y.Z; got {train!r}")
    if (int(match.group("major")), int(match.group("minor"))) != (major, minor):
        raise IdentityError(
            f"TestFlight train {train} cannot carry {version}: a train is one minor; "
            f"pin MEWLA_TESTFLIGHT_VERSION to the minor's first iOS release ({marketing_version(version)})"
        )
    return train


def next_testflight_version(train: str, next_version: str) -> str:
    """The train after preparing next_version: kept within the minor, else opened by this release."""
    try:
        return check_testflight_version(next_version, train)
    except IdentityError:
        return marketing_version(next_version)


def read_testflight_version(root: Path = ROOT) -> str:
    try:
        project = (root / IOS_PROJECT).read_text(encoding="utf-8")
    except FileNotFoundError as exc:
        raise IdentityError(f"missing {IOS_PROJECT}") from exc
    found = TRAIN_RE.findall(project)
    if len(found) != 1:
        raise IdentityError(f"{IOS_PROJECT} must set MEWLA_TESTFLIGHT_VERSION exactly once")
    return found[0]


def read_manifest(root: Path = ROOT) -> dict:
    path = root / SOURCE
    try:
        manifest = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise IdentityError(f"missing version source: {SOURCE}") from exc
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise IdentityError(f"malformed JSON in {SOURCE}: {exc}") from exc
    if not isinstance(manifest, dict):
        raise IdentityError(f"{SOURCE} must contain a JSON object")
    return manifest


def read_version(root: Path = ROOT) -> str:
    version = read_manifest(root).get("version")
    parse(version)
    return version


def identity(version: str, train: str | None = None) -> dict[str, object]:
    code = build_code(version)
    values: dict[str, object] = {
        "version": version,
        "marketing_version": marketing_version(version),
        "version_code": code,
        "ios_build_number": code,
        "package": PACKAGE,
        "ios_bundle_id": IOS_BUNDLE_IDS["production"],
        "ios_preview_bundle_id": IOS_BUNDLE_IDS["preview"],
        "source": SOURCE,
    }
    if train is not None:
        values["testflight_version"] = check_testflight_version(version, train)
    return values


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Print Mewla's release identity")
    parser.add_argument("field", nargs="?", help="print only this field")
    parser.add_argument("--root", type=Path, default=ROOT, help="repository root")
    parser.add_argument("--version", help=f"derive from this version instead of {SOURCE}")
    parser.add_argument("--json", action="store_true", help="print JSON")
    args = parser.parse_args(argv)
    try:
        version = args.version if args.version is not None else read_version(args.root)
        # Only what asks for the train reads it, so other fields work for any --version.
        wants_train = args.field in (None, "testflight_version")
        values = identity(version, read_testflight_version(args.root) if wants_train else None)
    except IdentityError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    if args.field:
        if args.field not in values:
            print(f"error: unknown field {args.field!r}; one of {', '.join(values)}", file=sys.stderr)
            return 2
        print(values[args.field])
    elif args.json:
        print(json.dumps(values, indent=2))
    else:
        for key, value in values.items():
            print(f"{key}={value}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
