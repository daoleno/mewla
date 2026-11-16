#!/usr/bin/env python3
"""Prepare a Mewla release as one deterministic tracked change: the version, nothing else.

The release notes are written at release time from the commits (scripts/release-body.py).
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import release_identity  # noqa: E402
import release_notes  # noqa: E402


BETA_VERSION_RE = re.compile(
    r"^(?P<major>0|[1-9][0-9]*)\."
    r"(?P<minor>0|[1-9][0-9]*)\."
    r"(?P<patch>0|[1-9][0-9]*)-beta\."
    r"(?P<beta>[1-9][0-9]*)$"
)
RELEASE_VERSION_RE = re.compile(
    r"^(?P<major>0|[1-9][0-9]*)\."
    r"(?P<minor>0|[1-9][0-9]*)\."
    r"(?P<patch>0|[1-9][0-9]*)"
    r"(?:-beta\.(?P<beta>[1-9][0-9]*))?$"
)
class PrepareError(RuntimeError):
    """A fail-closed preparation error."""


def next_beta_version(current: str) -> str:
    match = BETA_VERSION_RE.fullmatch(current)
    if match is None:
        raise PrepareError(
            f"current version must exactly match X.Y.Z-beta.N; got {current!r}"
        )
    return (
        f"{match.group('major')}.{match.group('minor')}.{match.group('patch')}"
        f"-beta.{int(match.group('beta')) + 1}"
    )


def next_release_version(current: str) -> str:
    beta = BETA_VERSION_RE.fullmatch(current)
    if beta is not None:
        return next_beta_version(current)
    match = RELEASE_VERSION_RE.fullmatch(current)
    if match is None:
        raise PrepareError(
            f"current version must exactly match X.Y.Z or X.Y.Z-beta.N; got {current!r}"
        )
    return (
        f"{match.group('major')}.{match.group('minor')}."
        f"{int(match.group('patch')) + 1}"
    )


def release_precedence(version: str) -> tuple[int, int, int, int, int]:
    match = RELEASE_VERSION_RE.fullmatch(version)
    if match is None:
        raise PrepareError(
            f"version must exactly match X.Y.Z or X.Y.Z-beta.N; got {version!r}"
        )
    beta = match.group("beta")
    return (
        int(match.group("major")),
        int(match.group("minor")),
        int(match.group("patch")),
        1 if beta is None else 0,
        int(beta or 0),
    )


def validate_target_version(current: str, target: str) -> str:
    current_order = release_precedence(current)
    target_order = release_precedence(target)
    if target_order <= current_order:
        raise PrepareError(
            f"target version must be newer than current version {current}; got {target}"
        )
    return target


def run_git(root: Path, *args: str, check: bool = True) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=root,
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if check and result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise PrepareError(f"git {' '.join(args)} failed: {detail}")
    return result.stdout.strip()


# The one version source and the project that carries what Xcode needs of it.
VERSION_SOURCE = release_identity.SOURCE
IOS_PROJECT = "client/ios/project.yml"

MIN_COMPUTER_VERSION_RE = re.compile(
    r'export const MIN_COMPUTER_VERSION = "(?P<version>[^"]+)";'
)


def validate_min_computer_version(root: Path, next_version: str) -> None:
    """The oldest computer the app accepts must not be newer than the release
    that ships the app: a release asking for a Mewla that does not exist yet
    would show every user "this computer needs an update"."""
    relative = "client/core/update/version.ts"
    match = MIN_COMPUTER_VERSION_RE.search(read_text(root, relative))
    if match is None:
        raise PrepareError(f"{relative} must declare MIN_COMPUTER_VERSION")
    minimum = match.group("version")
    if release_precedence(minimum) > release_precedence(next_version):
        raise PrepareError(
            f"{relative} MIN_COMPUTER_VERSION {minimum} is newer than the release "
            f"{next_version}; lower it to {next_version} or earlier before releasing"
        )


def read_text(root: Path, relative: str) -> str:
    path = root / relative
    if not path.is_file():
        raise PrepareError(f"missing required file: {relative}")
    return path.read_text(encoding="utf-8")


def read_json(root: Path, relative: str) -> dict:
    try:
        value = json.loads(read_text(root, relative))
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise PrepareError(f"malformed JSON in {relative}: {exc}") from exc
    if not isinstance(value, dict):
        raise PrepareError(f"{relative} must contain a JSON object")
    return value


def replace_literal(
    text: str, old: str, new: str, *, relative: str, expected_count: int = 1
) -> str:
    count = text.count(old)
    if count != expected_count:
        raise PrepareError(
            f"{relative}: expected {expected_count} occurrence(s) of {old!r}; "
            f"found {count}"
        )
    return text.replace(old, new)


def require_positive_int(value: object, *, label: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 1:
        raise PrepareError(f"{label} must be a positive integer; got {value!r}")
    return value


def require_annotated_tag(root: Path, tag: str) -> str:
    tag_type = run_git(root, "cat-file", "-t", f"refs/tags/{tag}", check=False)
    if tag_type != "tag":
        raise PrepareError(f"current tag {tag} is missing or is not annotated")
    commit = run_git(root, "rev-parse", f"refs/tags/{tag}^{{commit}}")
    ancestor = subprocess.run(
        ["git", "merge-base", "--is-ancestor", commit, "HEAD"],
        cwd=root,
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    if ancestor.returncode != 0:
        raise PrepareError(f"current tag {tag} is not an ancestor of HEAD")
    return commit


def commit_range(root: Path, current_tag: str) -> list[tuple[str, str]]:
    raw = run_git(root, "log", "--reverse", "--format=%H%x09%s", f"{current_tag}..HEAD")
    commits: list[tuple[str, str]] = []
    for line in raw.splitlines():
        sha, separator, subject = line.partition("\t")
        if (
            not separator
            or re.fullmatch(r"[0-9a-f]{40}", sha) is None
            or not subject.strip()
        ):
            raise PrepareError("git log returned a malformed commit record")
        commits.append((sha, subject.strip()))
    if not commits:
        raise PrepareError(f"no commits exist after {current_tag}")
    return commits


def atomic_write(path: Path, content: str) -> None:
    mode = path.stat().st_mode & 0o777 if path.exists() else 0o644
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        newline="",
        dir=path.parent,
        prefix=f".{path.name}.",
        delete=False,
    )
    temp_path = Path(handle.name)
    try:
        with handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temp_path, mode)
        os.replace(temp_path, path)
    except BaseException:
        temp_path.unlink(missing_ok=True)
        raise


def prepare(root: Path, target_version: str | None = None) -> dict[str, object]:
    root = root.resolve()
    git_root = Path(run_git(root, "rev-parse", "--show-toplevel")).resolve()
    if git_root != root:
        raise PrepareError(f"--repo must be the repository root: {git_root}")
    dirty = run_git(root, "status", "--porcelain=v1", "--untracked-files=all")
    if dirty:
        raise PrepareError(f"repository has unexpected dirty state:\n{dirty}")

    manifest = read_json(root, VERSION_SOURCE)
    current_version = manifest.get("version")
    if not isinstance(current_version, str):
        raise PrepareError(f"{VERSION_SOURCE} version must be a string")

    next_version = (
        next_release_version(current_version)
        if target_version is None
        else validate_target_version(current_version, target_version)
    )
    # The build numbers follow from the version (scripts/lib/release_identity.py).
    try:
        current_code = release_identity.build_code(current_version)
        next_code = release_identity.build_code(next_version)
    except release_identity.IdentityError as exc:
        raise PrepareError(str(exc)) from exc
    current_tag = f"v{current_version}"
    next_tag = f"v{next_version}"
    require_annotated_tag(root, current_tag)
    if run_git(root, "show-ref", "--verify", f"refs/tags/{next_tag}", check=False):
        raise PrepareError(f"next tag already exists: {next_tag}")

    commits = commit_range(root, current_tag)

    # What's New (release-notes/<version>.json) is optional: a release without one shows no sheet,
    # only its line in Brain's chat. The copies the app and the daemon bundle must be current.
    whats_new = ["--repo", str(root), "--check"]
    if (root / "release-notes" / f"{next_version}.json").is_file():
        whats_new += ["--require", next_version]
    if release_notes.main(whats_new) != 0:
        raise PrepareError(f"What's New notes for {next_version} are not current; see the error above")

    sources = {
        relative: read_text(root, relative)
        for relative in (
            VERSION_SOURCE,
            IOS_PROJECT,
            "daemon/cmd/mewla/version.go",
            "docs/install-daemon.md",
        )
    }
    validate_min_computer_version(root, next_version)

    updates: dict[str, str] = {}
    updates[VERSION_SOURCE] = replace_literal(
        sources[VERSION_SOURCE],
        f'"version": "{current_version}"',
        f'"version": "{next_version}"',
        relative=VERSION_SOURCE,
    )
    # Xcode reads no JSON: the iOS project carries the derived marketing version and build number.
    ios_project = replace_literal(
        sources[IOS_PROJECT],
        f'MARKETING_VERSION: "{release_identity.marketing_version(current_version)}"\n',
        f'MARKETING_VERSION: "{release_identity.marketing_version(next_version)}"\n',
        relative=IOS_PROJECT,
    )
    ios_project = replace_literal(
        ios_project,
        f'CURRENT_PROJECT_VERSION: "{current_code}"\n',
        f'CURRENT_PROJECT_VERSION: "{next_code}"\n',
        relative=IOS_PROJECT,
    )
    ios_project = replace_literal(
        ios_project,
        f'MEWLA_APP_VERSION: "{current_version}"\n',
        f'MEWLA_APP_VERSION: "{next_version}"\n',
        relative=IOS_PROJECT,
    )
    # The TestFlight train stays within a minor; a minor bump opens the next one with this release.
    try:
        current_train = release_identity.read_testflight_version(root)
    except release_identity.IdentityError as exc:
        raise PrepareError(str(exc)) from exc
    next_train = release_identity.next_testflight_version(current_train, next_version)
    if next_train != current_train:
        ios_project = replace_literal(
            ios_project,
            f'MEWLA_TESTFLIGHT_VERSION: "{current_train}"\n',
            f'MEWLA_TESTFLIGHT_VERSION: "{next_train}"\n',
            relative=IOS_PROJECT,
        )
    updates[IOS_PROJECT] = ios_project
    updates["daemon/cmd/mewla/version.go"] = replace_literal(
        sources["daemon/cmd/mewla/version.go"],
        f'var Version = "{current_version}"',
        f'var Version = "{next_version}"',
        relative="daemon/cmd/mewla/version.go",
    )

    updates["docs/install-daemon.md"] = replace_literal(
        sources["docs/install-daemon.md"],
        current_tag,
        next_tag,
        relative="docs/install-daemon.md",
    )
    originals = {
        relative: (root / relative).read_bytes() if (root / relative).exists() else None
        for relative in updates
    }
    written: list[str] = []
    try:
        for relative, content in updates.items():
            atomic_write(root / relative, content)
            written.append(relative)
    except BaseException:
        for relative in reversed(written):
            original = originals[relative]
            path = root / relative
            if original is None:
                path.unlink(missing_ok=True)
            else:
                atomic_write(path, original.decode("utf-8"))
        raise

    return {
        "current_tag": current_tag,
        "current_version": current_version,
        "next_tag": next_tag,
        "next_version": next_version,
        "android_version_code": next_code,
        "ios_build_number": next_code,
        "commit_count": len(commits),
        "changed_paths": sorted(updates),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Prepare a tracked Mewla release"
    )
    parser.add_argument(
        "--repo",
        type=Path,
        default=Path(__file__).resolve().parent.parent,
        help="repository root (defaults to this script's repository)",
    )
    parser.add_argument(
        "--version",
        help="explicit target X.Y.Z or X.Y.Z-beta.N (default: increment beta ordinal or stable patch)",
    )
    args = parser.parse_args(argv)
    try:
        result = prepare(args.repo, args.version)
    except PrepareError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
