#!/usr/bin/env python3
"""Checks that the web package's index.html loads every file from the site's root, so a deep link such as
/sessions/<id>, reloaded or opened, finds them where the daemon serves them (R5#2: a reload of
/sessions/<id> loaded the page's files from /sessions/ and spun forever).

NSH 0.6.0's page has no <base href> (a strict base-uri refuses it): under nsh.json web.base "/" it names
every file by an absolute URL and gives the player the base in the app's JSON. This checks exactly that:
the app's JSON says "base":"/", and every file the page names (its src and href attributes, and the files
it prefetches) starts with one "/". It fails on any other page, so an NSH change is seen, not shipped.

Usage: web-page-root.py <index.html or index.html.gz>
"""
import gzip
import json
import re
import sys

path = sys.argv[1]
raw = open(path, "rb").read()
html = (gzip.decompress(raw) if path.endswith(".gz") else raw).decode("utf-8")


def fail(why: str) -> None:
    sys.exit(f"{path}: {why}")


app = re.search(r'<script type="application/json" id="nsh-app">(.*?)</script>', html, re.S)
if not app:
    fail("no app JSON (<script id=\"nsh-app\">): not the page NSH 0.6 packages")
base = json.loads(app.group(1)).get("base")
if base != "/":
    fail(f"the app's base is {base!r}, not \"/\" (nsh.json web.base)")
if "<base " in html:
    fail("has a <base> element, which the daemon's base-uri and NSH 0.6's page do not expect")

urls = re.findall(r'\s(?:src|href)="([^"]*)"', html)
fetch = re.search(r'<script type="application/json" id="nsh-fetch">(.*?)</script>', html, re.S)
if fetch:
    urls += json.loads(fetch.group(1))
if not urls:
    fail("names no files")
relative = [url for url in urls if not url.startswith("/") or url.startswith("//")]
if relative:
    fail(f"names files that are not at the site's root: {relative[:5]}")
print(f"web-page-root: {len(urls)} files, all from /")
