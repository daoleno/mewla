#!/usr/bin/env python3
"""Build the public Mewla docs site.

Copies site/ to --out, then renders the pages listed in nav.json (plus every
docs/releases/v*.md) to <out>/docs/<slug>/index.html. Styling lives in
site/docs/docs.css and search in site/docs/search.js.

The build fails when a published page links to an unpublished file or contains
anything the public-safety scan rejects, and when any file copied from site/
uses the anonymised repository slug.

    python3 scripts/site-docs/build.py --out /tmp/mewla-site
"""

from __future__ import annotations

import argparse
import html
import json
import os
import re
import shutil
import sys
from dataclasses import dataclass, field
from pathlib import Path

import markdown
from markdown.extensions.toc import TocExtension

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
SITE = ROOT / "site"
DOCS = ROOT / "docs"

ALERT_KINDS = ("NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION")
ALERT_RE = re.compile(
    r"^> \[!(" + "|".join(ALERT_KINDS) + r")\][ \t]*\n((?:>.*(?:\n|$))*)", re.M
)
REPO_ONLY_RE = re.compile(r"<!-- repo-only -->.*?<!-- /repo-only -->\n?", re.S)
RELEASE_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?$")
HREF_RE = re.compile(r'(<a\b[^>]*?\bhref=")([^"]*)(")')
SRC_RE = re.compile(r'(<img\b[^>]*?\bsrc=")([^"]*)(")')
TAG_RE = re.compile(r"<[^>]+>")
BLOCK_TAG_RE = re.compile(r"</?(?:p|li|ul|ol|h[1-6]|pre|table|tr|td|th|div|blockquote|br|hr)\b[^>]*>")

PRIVATE_IP = (
    r"\b(?:10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}"
    r"|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2}"
    r"|100\.(?:6[4-9]|[7-9]\d|1[01]\d|12[0-7])(?:\.\d{1,3}){2})\b"
)
ANON_SLUG = (re.compile(r"\b(?:github|githubusercontent)\.com/user/", re.I), "anonymised repository slug")
UNSAFE = [
    (re.compile(r"/home/[A-Za-z0-9_.-]+"), "personal home path"),
    (re.compile(r"/Users/[A-Za-z0-9_.-]+"), "personal home path"),
    (re.compile(r"\b[A-Za-z0-9-]+\.ts\.net\b"), "tailnet hostname"),
    ANON_SLUG,
    (re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_\w{20,})"), "GitHub token"),
    (re.compile(r"\bxox[abprs]-[A-Za-z0-9-]{10,}"), "Slack token"),
    (re.compile(r"\bsk-[A-Za-z0-9_-]{20,}"), "API key"),
    (re.compile(r"\bAKIA[0-9A-Z]{16}\b"), "AWS access key"),
    (re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----"), "private key"),
    (re.compile(r"\b\d{8,10}:[A-Za-z0-9_-]{35}\b"), "Telegram bot token"),
    (re.compile(PRIVATE_IP), "private IP address"),
    (re.compile(r"\b[a-z0-9-]+\.trycloudflare\.com"), "tunnel URL"),
    (re.compile(r"#pair=[A-Za-z0-9_-]{16,}|(?:mewla|mewla)://[A-Za-z0-9_-]{16,}"), "pairing link"),
    (re.compile(r"\bw[0-9a-f]{32}\b"), "internal Work id"),
    (re.compile(r"\bturn:[0-9a-f]{8}-[0-9a-f]{4}-"), "internal turn id"),
]


class BuildError(Exception):
    pass


@dataclass
class Page:
    source: Path
    slug: str
    title: str
    group: str = ""
    release_index: bool = False
    in_nav: bool = True
    html: str = ""
    toc: list = field(default_factory=list)
    text: str = ""

    @property
    def dir(self) -> Path:
        return Path("docs") / self.slug if self.slug else Path("docs")

    @property
    def docs_root(self) -> str:
        depth = len(Path(self.slug).parts) if self.slug else 0
        return "../" * depth or "./"

    @property
    def site_root(self) -> str:
        return "../" * (len(self.dir.parts))

    @property
    def url(self) -> str:
        return f"{self.slug}/" if self.slug else ""


def gh_slug(value: str, separator: str = "-") -> str:
    value = re.sub(r"[^\w\- ]", "", value.strip().lower())
    return value.replace(" ", separator)


def release_key(tag: str):
    m = RELEASE_RE.match(tag)
    if not m:
        raise BuildError(f"docs/releases/{tag}.md: unsupported release tag")
    major, minor, patch, beta = m.groups()
    return (int(major), int(minor), int(patch), beta is None, int(beta or 0))


def load_pages(config: dict) -> list[Page]:
    home = config["home"]
    pages = [Page(ROOT / home["source"], "", home["title"], in_nav=False)]
    for group in config["groups"]:
        for item in group["pages"]:
            pages.append(
                Page(
                    ROOT / item["source"],
                    item["slug"],
                    item["title"],
                    group=group["title"],
                    release_index=item.get("release_index", False),
                )
            )
    tags = sorted(
        (p.stem for p in (DOCS / "releases").glob("v*.md")), key=release_key, reverse=True
    )
    for tag in tags:
        pages.append(
            Page(DOCS / "releases" / f"{tag}.md", f"releases/{tag}", f"Mewla {tag}", "Reference", in_nav=False)
        )
    for page in pages:
        if not page.source.is_file():
            raise BuildError(f"nav.json lists a missing source: {page.source.relative_to(ROOT)}")
    return pages


def release_list(pages: list[Page]) -> str:
    lines = []
    for page in pages:
        if page.slug.startswith("releases/"):
            tag = page.slug.split("/", 1)[1]
            kind = "beta" if "-beta." in tag else "stable"
            lines.append(f"- [{tag}]({tag}.md) ({kind})")
    return "\n".join(lines) + "\n"


def preprocess(text: str, page: Page, pages: list[Page]) -> str:
    text = REPO_ONLY_RE.sub("", text)
    if page.release_index:
        if "<!-- release-list -->" not in text:
            raise BuildError(f"{rel(page.source)}: missing <!-- release-list --> marker")
        text = text.replace("<!-- release-list -->", release_list(pages))

    def alert(m):
        kind = m.group(1).lower()
        body = re.sub(r"^> ?", "", m.group(2), flags=re.M)
        return f'\n<div class="callout callout-{kind}" markdown="1">\n\n{body}\n</div>\n\n'

    return ALERT_RE.sub(alert, text)


def rel(path: Path) -> str:
    return str(path.relative_to(ROOT))


def rewrite_links(body: str, page: Page, by_source: dict, assets: set) -> str:
    def target_for(href: str, attr: str) -> str:
        if href.startswith(("http://", "https://", "mailto:")):
            return href
        if href.startswith("#"):
            return href
        path, _, frag = href.partition("#")
        target = (page.source.parent / path).resolve()
        if target.is_dir():
            target = target / "README.md"
        suffix = f"#{frag}" if frag else ""
        if target in by_source:
            dest = by_source[target]
            link = os.path.relpath(dest.dir, page.dir).replace(os.sep, "/")
            return ("./" if link == "." else link + "/") + suffix
        if attr == "src" and target.is_file() and DOCS / "assets" in target.parents:
            assets.add(target)
            out = Path("docs") / target.relative_to(DOCS)
            return os.path.relpath(out, page.dir).replace(os.sep, "/")
        raise BuildError(f"{rel(page.source)}: link to unpublished file '{href}'")

    body = HREF_RE.sub(lambda m: m.group(1) + html.escape(target_for(html.unescape(m.group(2)), "href")) + m.group(3), body)
    return SRC_RE.sub(lambda m: m.group(1) + html.escape(target_for(html.unescape(m.group(2)), "src")) + m.group(3), body)


def render(page: Page, pages: list[Page], by_source: dict, assets: set) -> None:
    text = preprocess(page.source.read_text(encoding="utf-8"), page, pages)
    md = markdown.Markdown(
        extensions=[
            "tables",
            "fenced_code",
            "sane_lists",
            "md_in_html",
            TocExtension(slugify=gh_slug, toc_depth="2-3", permalink="#", permalink_class="anchor", permalink_title="Link to this section"),
        ]
    )
    body = md.convert(text)
    body = rewrite_links(body, page, by_source, assets)
    body = body.replace("<table>", '<div class="table-wrap"><table>').replace("</table>", "</table></div>")
    page.html = body.replace('<a class="anchor" ', '<a class="anchor" aria-label="Link to this section" ')
    page.toc = md.toc_tokens
    text = re.sub(r'<a class="anchor"[^>]*>.*?</a>', "", body)
    text = BLOCK_TAG_RE.sub(" ", text)
    page.text = re.sub(r"\s+", " ", html.unescape(TAG_RE.sub("", text))).strip()


def scan(page: Page) -> list[str]:
    raw = page.source.read_text(encoding="utf-8")
    problems = []
    for pattern, label in UNSAFE:
        for haystack in (raw, page.html):
            m = pattern.search(haystack)
            if m:
                problems.append(f"{rel(page.source)}: {label}: {m.group(0)!r}")
                break
    return problems


def scan_site() -> list[str]:
    pattern, label = ANON_SLUG
    problems = []
    for path in sorted(SITE.rglob("*")):
        if path.suffix in (".html", ".js", ".css", ".json", ".txt", ".xml"):
            m = pattern.search(path.read_text(encoding="utf-8", errors="replace"))
            if m:
                problems.append(f"{rel(path)}: {label}: {m.group(0)!r}")
    return problems


def nav_html(pages: list[Page], current: Page) -> str:
    out, group = [], None
    active = "releases" if current.slug.startswith("releases/") else current.slug
    for page in pages:
        if not page.in_nav:
            continue
        if page.group != group:
            if group is not None:
                out.append("</ul>")
            group = page.group
            out.append(f'<p class="d-group">{html.escape(group)}</p><ul>')
        href = os.path.relpath(page.dir, current.dir).replace(os.sep, "/") + "/"
        cur = ' aria-current="page"' if page.slug == active else ""
        out.append(f'<li><a href="{href}"{cur}>{html.escape(page.title)}</a></li>')
    out.append("</ul>")
    return "\n".join(out)


def toc_html(page: Page) -> str:
    items = [t for t in (page.toc[0]["children"] if len(page.toc) == 1 and page.toc[0]["level"] == 1 else page.toc) if t["level"] == 2]
    if len(items) < 2:
        return ""
    links = "".join(f'<li><a href="#{t["id"]}">{html.escape(html.unescape(t["name"]))}</a></li>' for t in items)
    return f'<aside class="d-toc" aria-label="On this page"><p class="d-group">On this page</p><ul>{links}</ul></aside>'


def pager_html(page: Page, nav_pages: list[Page]) -> str:
    if page not in nav_pages:
        return ""
    i = nav_pages.index(page)
    parts = []
    for label, other, cls in (("Previous", nav_pages[i - 1] if i > 0 else None, "prev"), ("Next", nav_pages[i + 1] if i + 1 < len(nav_pages) else None, "next")):
        if other:
            href = os.path.relpath(other.dir, page.dir).replace(os.sep, "/") + "/"
            parts.append(f'<a class="d-{cls}" href="{href}"><span>{label}</span>{html.escape(other.title)}</a>')
    return f'<nav class="d-pager" aria-label="Previous and next">{"".join(parts)}</nav>' if parts else ""


def description(page: Page) -> str:
    m = re.search(r"<p>(.*?)</p>", page.html, re.S)
    text = html.unescape(TAG_RE.sub("", m.group(1))) if m else page.title
    text = re.sub(r"\s+", " ", text).strip()
    return text if len(text) <= 160 else text[:157].rsplit(" ", 1)[0] + "…"


def icon_href() -> str:
    for candidate in ("favicon-32.png",):
        if (SITE / candidate).is_file():
            return candidate
    return ""


TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>{title}</title>
<meta name="description" content="{description}">
<meta name="color-scheme" content="light">
<meta name="theme-color" content="#fbfaf7">
{icon}<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,600;12..96,800&amp;family=Inter:wght@400;500;600&amp;family=JetBrains+Mono:wght@400;500&amp;display=swap">
<link rel="stylesheet" href="{docs_root}docs.css">
<script>document.documentElement.classList.add("js")</script>
<script defer src="{docs_root}search.js"></script>
</head>
<body data-docs-root="{docs_root}">
<a class="skip" href="#content">Skip to content</a>
<header class="d-top">
  <a class="d-word" href="{site_root}" aria-label="Mewla home">mewla</a>
  <a class="d-section" href="{docs_root}">docs</a>
  <button class="d-menu" type="button" aria-expanded="false" aria-controls="d-side">Menu</button>
</header>
<div class="d-shell">
  <div class="d-side" id="d-side">
    <form class="d-search" role="search" action="{docs_root}" onsubmit="return false">
      <label class="sr" for="d-q">Search the docs</label>
      <input id="d-q" type="search" placeholder="Search the docs" autocomplete="off" spellcheck="false">
      <ol class="d-results" id="d-results" hidden></ol>
    </form>
    <nav class="d-nav" aria-label="Documentation">
{nav}
    </nav>
  </div>
  <main class="d-main" id="content">
    <article class="d-doc">
{body}
    </article>
{pager}
    <footer class="d-foot">Mewla documentation · Apache-2.0 · <a href="{site_root}">Mewla home</a></footer>
  </main>
{toc}
</div>
</body>
</html>
"""


def write_page(out: Path, page: Page, pages: list[Page], nav_pages: list[Page], icon: str) -> None:
    title = "Mewla documentation" if not page.slug else f"{page.title} · Mewla docs"
    icon_tag = f'<link rel="icon" href="{page.site_root}{icon}" type="image/png">\n' if icon else ""
    doc = TEMPLATE.format(
        title=html.escape(title),
        description=html.escape(description(page)),
        icon=icon_tag,
        docs_root=page.docs_root,
        site_root=page.site_root or "./",
        nav=nav_html(pages, page),
        body=page.html,
        pager=pager_html(page, nav_pages),
        toc=toc_html(page),
    )
    target = out / page.dir / "index.html"
    if target.exists():
        raise BuildError(f"{target} already exists in site/; generated pages must not collide")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(doc, encoding="utf-8")


def search_index(pages: list[Page]) -> list[dict]:
    index = []
    for page in pages:
        headings = []

        def walk(tokens):
            for t in tokens:
                if t["level"] >= 2:
                    headings.append([html.unescape(t["name"]), t["id"]])
                walk(t["children"])

        walk(page.toc)
        index.append({"t": page.title, "u": page.url, "h": headings, "x": page.text[:6000]})
    return index


def build(out: Path) -> list[str]:
    config = json.loads((HERE / "nav.json").read_text(encoding="utf-8"))
    pages = load_pages(config)
    by_source = {p.source.resolve(): p for p in pages}
    if out.exists():
        shutil.rmtree(out)
    shutil.copytree(SITE, out)
    assets: set[Path] = set()
    problems = scan_site()
    for page in pages:
        render(page, pages, by_source, assets)
        problems.extend(scan(page))
    if problems:
        raise BuildError("public-safety scan failed:\n  " + "\n  ".join(problems))
    nav_pages = [p for p in pages if p.in_nav]
    icon = icon_href()
    for page in pages:
        write_page(out, page, pages, nav_pages, icon)
    for asset in sorted(assets):
        dest = out / "docs" / asset.relative_to(DOCS)
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(asset, dest)
    (out / "docs" / "search-index.json").write_text(
        json.dumps(search_index(pages), ensure_ascii=False, separators=(",", ":")), encoding="utf-8"
    )
    return [f"{len(pages)} pages written to {out}/docs"]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--out", required=True, type=Path, help="output directory (replaced)")
    args = parser.parse_args()
    out = args.out.resolve()
    if out == SITE or SITE in out.parents or out == ROOT:
        print("error: --out must be outside site/ and the repository root", file=sys.stderr)
        return 2
    try:
        for line in build(out):
            print(line)
    except BuildError as err:
        print(f"error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
