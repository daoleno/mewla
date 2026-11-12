#!/usr/bin/env python3
"""Make the terminal's bundled font from Maple Mono NF (client/native/terminal/fonts).

Maple Mono NF v7.9 (SIL OFL 1.1, no Reserved Font Name; Nerd Fonts glyphs patched in by the
Maple project), cut down to what a terminal cell needs:

- MapleMono-NF-Regular.ttf: everything but CJK (Latin, symbols, box drawing, blocks, braille,
  Powerline and Nerd Font icons, including Material Design in plane 15).
- MapleMono-NF-SemiBold.ttf (bold text): the same without the icons (they are drawn from Regular).
- MapleMono-NF-Italic.ttf and MapleMono-NF-SemiBoldItalic.ttf (italic, bold italic): only what italic
  text in a terminal is (ITALIC_RANGES: Latin, Latin-1, Latin Extended-A, Greek, Cyrillic, combining
  marks, punctuation, currency, arrows). Anything else in an italic run is drawn from the upright face,
  slanted.

CJK is left to the system face (PingFang, Noto Sans CJK); the renderers centre it in its two cells.
Hinting is dropped (phones draw at high density), and so is every layout feature but the ones that
place marks (ccmp, locl, mark, mkmk): the ligatures and alternates (calt, liga, cvNN, ssNN) would
merge cells (`->`, `!=`) on any platform that applies them. Metrics, names and advances stay.

Usage (fontTools installed; the zip is the upstream release, pinned by SHA-256 below):

  python3 scripts/subset-terminal-font.py MapleMono-NF.zip client/native/terminal/fonts
"""

import hashlib
import io
import sys
import zipfile
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont

ZIP_SHA256 = "59098b87c895d871635d37680e88000ae2b2b25b55428195b228ec589e35fb89"  # v7.9 MapleMono-NF.zip

CJK_RANGES = (
    (0x1100, 0x11FF), (0x2E80, 0x2FFF), (0x3000, 0x33FF), (0x3400, 0x4DBF), (0x4E00, 0x9FFF),
    (0xA960, 0xA97F), (0xAC00, 0xD7FF), (0xF900, 0xFAFF), (0xFE10, 0xFE1F), (0xFE30, 0xFE4F),
    (0xFF00, 0xFFEF), (0x20000, 0x3FFFF),
)
ICON_RANGES = ((0xE000, 0xF8FF), (0xF0000, 0x10FFFF))
ITALIC_RANGES = (
    (0x20, 0x7E), (0xA0, 0xFF), (0x100, 0x17F), (0x300, 0x36F), (0x370, 0x3FF), (0x400, 0x4FF),
    (0x2000, 0x206F), (0x20A0, 0x20CF), (0x2190, 0x21FF),
)
KEEP_FEATURES = ["ccmp", "locl", "mark", "mkmk"]
# Every cell the renderers do not draw themselves must come from this face.
MUST_HAVE = [*range(0x20, 0x7F), 0x23BF, 0x25CF, 0x276F, 0x2192, 0x2026, 0x2713, 0xE0A0, 0xE0A1, 0xE0A2,
             0xF00C, 0xF002, 0xF07B, 0xF015, 0xE725, 0xF0219]


def in_ranges(c, ranges):
    return any(low <= c <= high for low, high in ranges)


def subset_font(data: bytes, output: Path, icons: bool, italic: bool = False) -> None:
    font = TTFont(io.BytesIO(data))
    if italic:
        keep = [c for c in font.getBestCmap() if in_ranges(c, ITALIC_RANGES)]
    else:
        keep = [c for c in font.getBestCmap() if not in_ranges(c, CJK_RANGES) and (icons or not in_ranges(c, ICON_RANGES))]
    options = subset.Options()
    options.layout_features = KEEP_FEATURES
    options.name_IDs = ["*"]
    options.name_languages = ["*"]
    options.name_legacy = True
    options.notdef_outline = True
    options.hinting = False
    options.desubroutinize = True
    subsetter = subset.Subsetter(options)
    subsetter.populate(unicodes=keep)
    subsetter.subset(font)
    font.save(output)
    verify(TTFont(io.BytesIO(data)), TTFont(output), output, keep, icons, italic)


def verify(before: TTFont, after: TTFont, output: Path, keep, icons: bool, italic: bool) -> None:
    for table, fields in {"head": ["unitsPerEm"], "hhea": ["ascent", "descent", "lineGap"],
                          "OS/2": ["sTypoAscender", "sTypoDescender", "sTypoLineGap", "usWinAscent", "usWinDescent"]}.items():
        for field in fields:
            assert getattr(before[table], field) == getattr(after[table], field), (table, field)
    old, new = before.getBestCmap(), after.getBestCmap()
    assert set(new) == set(keep), "kept set differs"
    for c, glyph in new.items():
        # One cell, or zero for a combining mark.
        assert after["hmtx"][glyph][0] == before["hmtx"][old[c]][0] in (0, 600), hex(c)
    for c in MUST_HAVE:
        if (icons or not in_ranges(c, ICON_RANGES)) and (not italic or in_ranges(c, ITALIC_RANGES)):
            assert c in new, hex(c)
    features = {r.FeatureTag for r in after["GSUB"].table.FeatureList.FeatureRecord} if "GSUB" in after else set()
    assert not features & {"calt", "liga", "dlig", "rlig"}, features
    print(f"{output.name}: {len(old)} -> {len(new)} code points, {output.stat().st_size} bytes")


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    archive, out = Path(sys.argv[1]), Path(sys.argv[2])
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    if digest != ZIP_SHA256:
        sys.exit(f"{archive}: SHA-256 {digest} is not the pinned v7.9 MapleMono-NF.zip")
    out.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive) as z:
        subset_font(z.read("MapleMono-NF-Regular.ttf"), out / "MapleMono-NF-Regular.ttf", icons=True)
        subset_font(z.read("MapleMono-NF-SemiBold.ttf"), out / "MapleMono-NF-SemiBold.ttf", icons=False)
        subset_font(z.read("MapleMono-NF-Italic.ttf"), out / "MapleMono-NF-Italic.ttf", icons=False, italic=True)
        subset_font(z.read("MapleMono-NF-SemiBoldItalic.ttf"), out / "MapleMono-NF-SemiBoldItalic.ttf", icons=False, italic=True)
        (out / "OFL.txt").write_bytes(z.read("LICENSE.txt"))


if __name__ == "__main__":
    main()
