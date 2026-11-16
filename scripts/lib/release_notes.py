#!/usr/bin/env python3
"""What's New notes: check release-notes/<version>.json and write the copies the app and the daemon ship.

One file per release at release-notes/<version>.json is the source. The app (client/ui) and the daemon
(daemon/whatsnew) cannot read outside their own directories, so this script writes one bundle of every
release into each: client/ui/whatsnew/notes.json (the app and the web UI) and daemon/whatsnew/notes.json
(the daemon, for an app older than its computer). `--check` fails when a file is malformed or a bundle
is not what this script would write; `--require VERSION` also fails when that version has no file
(scripts/prepare-release.py runs it for the version it releases).
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SOURCE_DIR = "release-notes"
BUNDLES = ("client/ui/whatsnew/notes.json", "daemon/whatsnew/notes.json")

VERSION_RE = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.([1-9][0-9]*))?$")
DATE_RE = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}$")
ID_RE = re.compile(r"^[a-z0-9][a-z0-9.-]{2,63}$")
SCENE_RE = re.compile(r"^[a-z][a-z0-9-]{1,39}$")
# Where "Try it" can go (client/ui/whatsnew/tryIt.ts opens each one).
OPEN_TARGETS = ("brain", "sessions", "new-session", "screen", "calendar", "computer", "settings")
TRY_KINDS = ("open", "prompt")
MAX_TITLE_WORDS = 6
MAX_LINE = 90
MAX_HIGHLIGHTS = 4

SHAPE = """{
  "version": "X.Y.Z",
  "date": "YYYY-MM-DD",
  "highlights": [
    {
      "id": "X.Y.Z-short-name",
      "title": "At most six words",
      "line": "One short line of what the person can now do.",
      "media": {"scene": "a-scene-in-client/ui/whatsnew/scenes"},
      "try": {"kind": "open", "target": "screen"}
    }
  ],
  "fixes": ["A fix, in the person's words."]
}
"highlights" may be empty (a fixes-only release shows no sheet, only the line in Brain's chat)."""


class NotesError(RuntimeError):
    pass


def version_key(version: str) -> tuple[int, int, int, int]:
    match = VERSION_RE.match(version)
    if not match:
        raise NotesError(f"not a version: {version!r}")
    major, minor, patch, beta = match.groups()
    return (int(major), int(minor), int(patch), int(beta) if beta else 1_000_000)


def check_text(value: object, where: str, *, limit: int) -> str:
    if not isinstance(value, str) or not value.strip() or value != value.strip():
        raise NotesError(f"{where} must be a non-empty string without outer spaces")
    if "\n" in value:
        raise NotesError(f"{where} must be one line")
    if len(value) > limit:
        raise NotesError(f"{where} is {len(value)} characters; keep it to {limit}")
    if "…" in value or "..." in value:
        raise NotesError(f"{where} must not trail off with an ellipsis")
    return value


def check_release(path: Path, seen_ids: set[str]) -> dict:
    where = f"{SOURCE_DIR}/{path.name}"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise NotesError(f"{where}: {exc}") from exc
    if not isinstance(data, dict):
        raise NotesError(f"{where}: must be an object:\n{SHAPE}")
    unknown = set(data) - {"version", "date", "highlights", "fixes"}
    if unknown:
        raise NotesError(f"{where}: unknown keys {sorted(unknown)}:\n{SHAPE}")
    version = data.get("version")
    if not isinstance(version, str) or path.name != f"{version}.json":
        raise NotesError(f"{where}: version must be the file's name ({path.stem})")
    version_key(version)
    if not isinstance(data.get("date"), str) or not DATE_RE.match(data["date"]):
        raise NotesError(f"{where}: date must be YYYY-MM-DD")
    highlights = data.get("highlights")
    if not isinstance(highlights, list) or len(highlights) > MAX_HIGHLIGHTS:
        raise NotesError(f"{where}: highlights must be a list of at most {MAX_HIGHLIGHTS}:\n{SHAPE}")
    out_highlights = []
    for index, item in enumerate(highlights):
        at = f"{where}: highlights[{index}]"
        if not isinstance(item, dict):
            raise NotesError(f"{at} must be an object")
        unknown = set(item) - {"id", "title", "line", "media", "try"}
        if unknown:
            raise NotesError(f"{at}: unknown keys {sorted(unknown)}")
        hid = item.get("id")
        if not isinstance(hid, str) or not ID_RE.match(hid) or not hid.startswith(f"{version}-"):
            raise NotesError(f"{at}.id must be '{version}-<name>' in lowercase letters, digits, '-' and '.'")
        if hid in seen_ids:
            raise NotesError(f"{at}.id {hid!r} is used twice")
        seen_ids.add(hid)
        title = check_text(item.get("title"), f"{at}.title", limit=40)
        if len(title.split()) > MAX_TITLE_WORDS:
            raise NotesError(f"{at}.title has more than {MAX_TITLE_WORDS} words")
        if re.search(r"\b[0-9]+\.[0-9]+\.[0-9]+\b", title):
            raise NotesError(f"{at}.title must not name a version (the sheet shows it apart)")
        line = check_text(item.get("line"), f"{at}.line", limit=MAX_LINE)
        media = item.get("media")
        if not isinstance(media, dict) or set(media) != {"scene"} or not isinstance(media["scene"], str) \
                or not SCENE_RE.match(media["scene"]):
            raise NotesError(f"{at}.media must be {{\"scene\": \"<name>\"}}")
        out = {"id": hid, "title": title, "line": line, "media": {"scene": media["scene"]}}
        if "try" in item:
            action = item["try"]
            if not isinstance(action, dict) or set(action) != {"kind", "target"} or action["kind"] not in TRY_KINDS:
                raise NotesError(f"{at}.try must be {{\"kind\": one of {list(TRY_KINDS)}, \"target\": ...}}")
            if action["kind"] == "open" and action["target"] not in OPEN_TARGETS:
                raise NotesError(f"{at}.try.target must be one of {list(OPEN_TARGETS)}")
            if action["kind"] == "prompt":
                check_text(action["target"], f"{at}.try.target", limit=200)
            out["try"] = {"kind": action["kind"], "target": action["target"]}
        out_highlights.append(out)
    fixes = data.get("fixes", [])
    if not isinstance(fixes, list):
        raise NotesError(f"{where}: fixes must be a list of lines")
    out_fixes = [check_text(fix, f"{where}: fixes[{i}]", limit=160) for i, fix in enumerate(fixes)]
    if not out_highlights and not out_fixes:
        raise NotesError(f"{where}: give at least one highlight or fix")
    return {"version": version, "date": data["date"], "highlights": out_highlights, "fixes": out_fixes}


def load(root: Path) -> list[dict]:
    source = root / SOURCE_DIR
    paths = sorted(source.glob("*.json")) if source.is_dir() else []
    stray = sorted(p.name for p in source.iterdir() if p.suffix != ".json" and p.name != "README.md") \
        if source.is_dir() else []
    if stray:
        raise NotesError(f"{SOURCE_DIR}/ holds only <version>.json and README.md; found {stray}")
    seen: set[str] = set()
    releases = [check_release(path, seen) for path in paths]
    return sorted(releases, key=lambda r: version_key(r["version"]), reverse=True)


def bundle_text(releases: list[dict]) -> str:
    return json.dumps({"releases": releases}, indent=2, ensure_ascii=False) + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--repo", type=Path, default=ROOT)
    parser.add_argument("--check", action="store_true", help="fail when a bundle is not current")
    parser.add_argument("--require", metavar="VERSION", help="fail unless release-notes/VERSION.json exists")
    args = parser.parse_args(argv)
    root = args.repo.resolve()
    try:
        releases = load(root)
        if args.require is not None:
            version_key(args.require)
            if not any(r["version"] == args.require for r in releases):
                raise NotesError(
                    f"missing {SOURCE_DIR}/{args.require}.json: write the What's New notes for "
                    f"{args.require} before releasing it (highlights may be empty):\n{SHAPE}"
                )
        text = bundle_text(releases)
        stale = []
        for relative in BUNDLES:
            path = root / relative
            current = path.read_text(encoding="utf-8") if path.is_file() else None
            if current == text:
                continue
            if args.check:
                stale.append(relative)
            else:
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text, encoding="utf-8")
        if stale:
            raise NotesError(f"not current: {', '.join(stale)}; run python3 scripts/release-notes.py")
    except NotesError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
