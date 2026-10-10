#!/usr/bin/env python3
"""Prepare a Mewla release as one deterministic tracked change."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path


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
CHANGELOG_ENTRY_RE = re.compile(
    r"^- \[(v[0-9]+\.[0-9]+\.[0-9]+(?:-beta\.[1-9][0-9]*)?)\]"
    r"\(docs/releases/\1\.md\)$",
    re.MULTILINE,
)
CHANGELOG_PREFIX = """# Changelog

Canonical release notes live under [`docs/releases/`](docs/releases/). This file is a reverse-chronological index and does not duplicate the full notes.

## Releases

"""
REPOSITORY_URL = "https://github.com/daoleno/mewla"
INSTALL_SCRIPT_URL = "https://raw.githubusercontent.com/daoleno/mewla/main/install.sh"
INSTALL_DOCS_URL = "https://daoleno.github.io/mewla/docs/install/"
USER_NOTE_SECTIONS = ("New", "Fixed")
USER_NOTES_SHAPE = """<one plain sentence: what this release is about, for a user>

## New

- <user-facing change, in user language>

## Fixed

- <user-facing fix>

Keep at least one of ## New and ## Fixed; drop the one with nothing in it."""


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


def validate_changelog(
    root: Path, content: str, current_version: str, next_tag: str
) -> None:
    if not content.startswith(CHANGELOG_PREFIX):
        raise PrepareError("CHANGELOG.md does not match the canonical index header")
    entries = CHANGELOG_ENTRY_RE.findall(content)
    current_tag = f"v{current_version}"
    if not entries or entries[0] != current_tag:
        raise PrepareError(
            f"CHANGELOG.md must start with the current release {current_tag}; "
            f"got {entries[:1]!r}"
        )
    if len(entries) != len(set(entries)):
        raise PrepareError("CHANGELOG.md contains duplicate release entries")
    if content.count("\n- [") != len(entries):
        raise PrepareError("CHANGELOG.md contains malformed release entries")

    order = [release_precedence(tag.removeprefix("v")) for tag in entries]
    if any(left <= right for left, right in zip(order, order[1:])):
        raise PrepareError(
            "CHANGELOG.md release entries are not strictly reverse chronological"
        )
    if next_tag in content:
        raise PrepareError(f"CHANGELOG.md already contains {next_tag}")
    for tag in entries:
        note = root / "docs" / "releases" / f"{tag}.md"
        if not note.is_file():
            raise PrepareError(f"CHANGELOG.md points to missing canonical notes: {note}")


def extract_certificate(verifier_source: str) -> str:
    matches = re.findall(
        r'^EXPECTED_CERT_FP="([0-9A-F]{2}(?::[0-9A-F]{2}){31})"$',
        verifier_source,
        re.MULTILINE,
    )
    if len(matches) != 1:
        raise PrepareError(
            "scripts/verify-release-identity.sh must contain exactly one "
            f"EXPECTED_CERT_FP SHA-256 fingerprint; found {len(matches)}"
        )
    return matches[0]


def parse_user_notes(text: str, relative: str) -> tuple[str, dict[str, str]]:
    """Split the human-written notes into a summary and New/Fixed bullets."""

    def fail(problem: str) -> PrepareError:
        return PrepareError(
            f"{relative}: {problem}. Write it before dispatching the release:\n\n"
            f"{USER_NOTES_SHAPE}"
        )

    summary_lines: list[str] = []
    sections: dict[str, list[str]] = {}
    current: list[str] | None = None
    for line in text.strip().splitlines():
        if line.startswith("#"):
            heading = line.strip()
            name = heading.removeprefix("## ")
            if heading != f"## {name}" or name not in USER_NOTE_SECTIONS:
                raise fail(f"unexpected heading {heading!r}; use only ## New and ## Fixed")
            if name in sections:
                raise fail(f"duplicate ## {name} section")
            current = sections[name] = []
        elif current is None:
            if line.lstrip().startswith(("- ", "* ")):
                raise fail("put bullets under ## New or ## Fixed, not in the summary")
            summary_lines.append(line)
        elif line.strip():
            continuation = line.startswith("  ") and bool(current)
            if not (line.startswith("- ") or continuation):
                raise fail(f"## sections may contain only '- ' bullets; got {line!r}")
            current.append(line.rstrip())

    summary = "\n".join(summary_lines).strip()
    if not summary:
        raise fail("missing the one-sentence summary before ## New / ## Fixed")
    if "\n\n" in summary:
        raise fail("the summary must be one short paragraph")
    for name, bullets in sections.items():
        if not bullets:
            raise fail(f"## {name} has no bullets; remove it or add one")
    if not sections:
        raise fail("missing ## New or ## Fixed bullets")
    return summary, {
        name: "\n".join(sections[name])
        for name in USER_NOTE_SECTIONS
        if name in sections
    }


def build_release_notes(
    *,
    next_version: str,
    next_tag: str,
    current_tag: str,
    user_notes: str,
    user_notes_path: str,
    certificate: str,
) -> str:
    summary, sections = parse_user_notes(user_notes, user_notes_path)
    changes = "".join(
        f"## {name}\n\n{bullets}\n\n" for name, bullets in sections.items()
    )
    return f"""# Mewla {next_version}

{summary}

{changes}## Get it

- **Already installed?** Run `mewla update` on your computer, and install the new APK on Android.
- **New here?** `curl -fsSL {INSTALL_SCRIPT_URL} | sh` picks the right file for you. See [Install]({INSTALL_DOCS_URL}).
- **Mac (Apple Silicon):** `mewla-darwin-arm64.tar.gz`. Needs `tmux` (`brew install tmux`).
- **Linux x64:** `mewla-linux-amd64.tar.gz`
- **Linux ARM:** `mewla-linux-arm64.tar.gz`
- **Android:** `mewla-android-arm64-{next_tag}.apk`. Android asks you to allow installs from unknown sources.

All changes: [{current_tag}...{next_tag}]({REPOSITORY_URL}/compare/{current_tag}...{next_tag})

<details markdown="1">
<summary>Verify downloads</summary>

`mewla update` and the installer check downloads for you. To check one by hand against `SHA256SUMS`:

```sh
grep 'mewla-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -        # Linux
grep 'mewla-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c -   # Mac
```

Android signing certificate SHA-256:

```text
{certificate}
```

</details>
"""


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

    base = read_json(root, "app/app.base.json")
    try:
        expo = base["expo"]
        android = expo["android"]
        current_version = expo["version"]
        android_package = android["package"]
        android_version_code = require_positive_int(
            android["versionCode"], label="app/app.base.json Android versionCode"
        )
    except (KeyError, TypeError) as exc:
        raise PrepareError(f"app/app.base.json is missing release identity: {exc}") from exc
    if not isinstance(current_version, str):
        raise PrepareError("app/app.base.json expo.version must be a string")
    if not isinstance(android_package, str) or not android_package.strip():
        raise PrepareError("app/app.base.json Android package must be a non-empty string")

    next_version = (
        next_release_version(current_version)
        if target_version is None
        else validate_target_version(current_version, target_version)
    )
    current_tag = f"v{current_version}"
    next_tag = f"v{next_version}"
    require_annotated_tag(root, current_tag)
    if run_git(root, "show-ref", "--verify", f"refs/tags/{next_tag}", check=False):
        raise PrepareError(f"next tag already exists: {next_tag}")

    next_notes_relative = f"docs/releases/{next_tag}.md"
    if (root / next_notes_relative).exists():
        raise PrepareError(f"next release notes already exist: {next_notes_relative}")
    commits = commit_range(root, current_tag)

    ios_doc = read_json(root, "app/ios-build.json")
    ios_build = require_positive_int(
        ios_doc.get("buildNumber"), label="app/ios-build.json buildNumber"
    )
    next_version_code = android_version_code + 1
    next_ios_build = ios_build + 1

    user_notes_relative = f"docs/releases/reviewed/{next_tag}.md"
    if not (root / user_notes_relative).is_file():
        raise PrepareError(
            f"missing {user_notes_relative}. Commit the user-facing notes for "
            f"{next_tag} before dispatching the release:\n\n{USER_NOTES_SHAPE}"
        )
    user_notes = read_text(root, user_notes_relative)
    parse_user_notes(user_notes, user_notes_relative)

    sources = {
        relative: read_text(root, relative)
        for relative in (
            "app/app.base.json",
            "app/ios-build.json",
            "daemon/cmd/mewla/version.go",
            "scripts/verify-release-identity.sh",
            "docs/install-daemon.md",
            "docs/ios-ci-release.md",
            "CHANGELOG.md",
        )
    }
    validate_changelog(root, sources["CHANGELOG.md"], current_version, next_tag)
    certificate = extract_certificate(
        sources["scripts/verify-release-identity.sh"]
    )

    updates: dict[str, str] = {}
    updates["app/app.base.json"] = replace_literal(
        sources["app/app.base.json"],
        f'"version": "{current_version}"',
        f'"version": "{next_version}"',
        relative="app/app.base.json",
    )
    updates["app/app.base.json"] = replace_literal(
        updates["app/app.base.json"],
        f'"versionCode": {android_version_code}',
        f'"versionCode": {next_version_code}',
        relative="app/app.base.json",
    )
    updates["app/ios-build.json"] = replace_literal(
        sources["app/ios-build.json"],
        f'"buildNumber": {ios_build}',
        f'"buildNumber": {next_ios_build}',
        relative="app/ios-build.json",
    )
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
    ios_docs = replace_literal(
        sources["docs/ios-ci-release.md"],
        f"tracked general/Android version is `{current_version}`",
        f"tracked general/Android version is `{next_version}`",
        relative="docs/ios-ci-release.md",
    )
    ios_docs = replace_literal(
        ios_docs,
        f"marketing version `{current_version.split('-', 1)[0]}`",
        f"marketing version `{next_version.split('-', 1)[0]}`",
        relative="docs/ios-ci-release.md",
    )
    ios_docs = replace_literal(
        ios_docs,
        f"baseline is build `{ios_build}` in `app/ios-build.json`",
        f"baseline is build `{next_ios_build}` in `app/ios-build.json`",
        relative="docs/ios-ci-release.md",
    )
    updates["docs/ios-ci-release.md"] = ios_docs

    changelog_entry = f"- [{next_tag}](docs/releases/{next_tag}.md)\n"
    updates["CHANGELOG.md"] = replace_literal(
        sources["CHANGELOG.md"],
        CHANGELOG_PREFIX,
        CHANGELOG_PREFIX + changelog_entry,
        relative="CHANGELOG.md",
    )
    updates[next_notes_relative] = build_release_notes(
        next_version=next_version,
        next_tag=next_tag,
        current_tag=current_tag,
        user_notes=user_notes,
        user_notes_path=user_notes_relative,
        certificate=certificate,
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
        "android_version_code": next_version_code,
        "ios_build_number": next_ios_build,
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
