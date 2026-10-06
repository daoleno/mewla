#!/usr/bin/env python3
"""Check links in the built site or in the repository's Markdown docs.

    python3 scripts/site-docs/check_links.py site /tmp/zen-site [--external]
    python3 scripts/site-docs/check_links.py repo

site: every relative href/src in every HTML file must resolve to a file in the
build, and every #fragment must match an id in its target. --external also
requests each external URL once.

repo: every relative link in docs/**/*.md and the top-level Markdown files must
resolve to a file, and #fragments into Markdown must match a heading.
"""

from __future__ import annotations

import re
import sys
import urllib.error
import urllib.request
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[2]
TOP_LEVEL_MD = ["README.md", "CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md", "TRADEMARKS.md"]


class Links(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.ids: set[str] = set()
        self.links: list[str] = []

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        for key in ("id", "name") if tag == "a" else ("id",):
            if a.get(key):
                self.ids.add(a[key])
        for key in ("href", "src"):
            if a.get(key) and not (tag == "link" and a.get("rel") in ("preconnect",)):
                self.links.append(a[key])


def check_site(out: Path, external: bool) -> int:
    pages: dict[Path, Links] = {}
    for f in out.rglob("*.html"):
        parser = Links()
        parser.feed(f.read_text(encoding="utf-8"))
        pages[f.resolve()] = parser
    broken, externals, checked = [], set(), 0
    for f, parser in sorted(pages.items()):
        for link in parser.links:
            parts = urlsplit(link)
            if parts.scheme in ("http", "https"):
                externals.add(link)
                continue
            if parts.scheme in ("mailto", "data", "javascript") or link.startswith("//"):
                continue
            checked += 1
            target = f if not parts.path else (f.parent / unquote(parts.path)).resolve()
            if target.is_dir():
                target = target / "index.html"
            if out.resolve() not in target.parents:
                broken.append(f"{f.relative_to(out)}: {link} (outside the site)")
                continue
            if not target.exists():
                broken.append(f"{f.relative_to(out)}: {link} (missing file)")
                continue
            if parts.fragment and target.suffix == ".html":
                ids = pages[target].ids if target in pages else set()
                if unquote(parts.fragment) not in ids:
                    broken.append(f"{f.relative_to(out)}: {link} (missing anchor)")
    print(f"site: {len(pages)} HTML files, {checked} internal links checked, {len(broken)} broken")
    for line in broken:
        print(f"  BROKEN {line}")
    bad_external = []
    if external:
        for url in sorted(externals):
            status = fetch(url)
            ok = isinstance(status, int) and status < 400
            print(f"  {'ok ' if ok else 'BAD'} {status} {url}")
            if not ok:
                bad_external.append(url)
    else:
        print(f"site: {len(externals)} external URLs not fetched (use --external)")
    return 1 if broken or bad_external else 0


def fetch(url: str):
    headers = {"User-Agent": "zen-docs-linkcheck/1"}
    for method in ("HEAD", "GET"):
        try:
            req = urllib.request.Request(url, method=method, headers=headers)
            with urllib.request.urlopen(req, timeout=15) as resp:
                return resp.status
        except urllib.error.HTTPError as err:
            if method == "HEAD" and err.code in (403, 405, 429):
                continue
            return err.code
        except Exception as err:  # noqa: BLE001 - report any network failure
            return type(err).__name__
    return "error"


FENCE_RE = re.compile(r"^(```|~~~).*?^\1", re.S | re.M)
INLINE_CODE_RE = re.compile(r"`[^`\n]*`")
MD_LINK_RE = re.compile(r"!?\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
HTML_LINK_RE = re.compile(r'<(?:a|img)\b[^>]*?\b(?:href|src)="([^"]+)"')
HEADING_RE = re.compile(r"^#{1,6}\s+(.*?)\s*#*\s*$", re.M)
EXPLICIT_ANCHOR_RE = re.compile(r'<a\s+(?:id|name)="([^"]+)"')


def gh_slug(text: str) -> str:
    text = re.sub(r"<[^>]+>", "", text)
    text = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", text)
    text = text.replace("`", "").replace("*", "")
    text = re.sub(r"[^\w\- ]", "", text.strip().lower())
    return text.replace(" ", "-")


def anchors(md: Path) -> set[str]:
    text = FENCE_RE.sub("", md.read_text(encoding="utf-8"))
    seen: dict[str, int] = {}
    out = set(EXPLICIT_ANCHOR_RE.findall(text))
    for heading in HEADING_RE.findall(text):
        slug = gh_slug(heading)
        n = seen.get(slug, 0)
        out.add(slug if n == 0 else f"{slug}-{n}")
        seen[slug] = n + 1
    return out


def check_repo() -> int:
    files = sorted((ROOT / "docs").rglob("*.md")) + [ROOT / f for f in TOP_LEVEL_MD if (ROOT / f).exists()]
    broken, checked = [], 0
    cache: dict[Path, set[str]] = {}
    for md in files:
        text = INLINE_CODE_RE.sub("", FENCE_RE.sub("", md.read_text(encoding="utf-8")))
        for link in MD_LINK_RE.findall(text) + HTML_LINK_RE.findall(text):
            parts = urlsplit(link)
            if parts.scheme or link.startswith("//") or "{{" in link:
                continue
            checked += 1
            target = md if not parts.path else (md.parent / unquote(parts.path)).resolve()
            where = md.relative_to(ROOT)
            if not target.exists():
                broken.append(f"{where}: {link} (missing file)")
                continue
            if parts.fragment and target.suffix == ".md":
                if target not in cache:
                    cache[target] = anchors(target)
                if parts.fragment not in cache[target]:
                    broken.append(f"{where}: {link} (missing anchor)")
    print(f"repo: {len(files)} Markdown files, {checked} relative links checked, {len(broken)} broken")
    for line in broken:
        print(f"  BROKEN {line}")
    return 1 if broken else 0


def main(argv: list[str]) -> int:
    if len(argv) >= 2 and argv[0] == "site":
        return check_site(Path(argv[1]), "--external" in argv[2:])
    if argv == ["repo"]:
        return check_repo()
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
