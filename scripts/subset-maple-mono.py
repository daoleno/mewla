#!/usr/bin/env python3
"""Subset Maple Mono CN to the glyphs the native terminal must own.

Drops every CJK-script codepoint (Han, kana, Hangul, bopomofo, CJK
punctuation, fullwidth forms) and keeps the rest: Latin, box drawing,
blocks, braille, arrows, dingbats, Powerline/Nerd icons. CJK text comes from
the OS face (PingFang on iOS, Noto Sans CJK on Android); the terminal
formatter pins those cells to two columns, so the grid never depends on the
fallback face's advance. The name table, metrics, hinting and layout
features are unchanged, so the face keeps its PostScript name and 0.6em cell.

Usage (from the repository root, with fontTools installed):

  python3 scripts/subset-maple-mono.py <dir with upstream TTFs> app/assets/fonts

The input is the upstream Maple Mono CN v7.900 release files
`MapleMono-CN-{Regular,SemiBold}.ttf`, pinned by SHA-256 below. Git history
before commit 9d6be668 holds the same files.
"""

import hashlib
import sys
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont

SOURCES = {
    "MapleMono-CN-Regular.ttf": "e6338c3dca9a3bed3fb81f43c8bb8feca3b6ad54829f5a3229527d030a321dbd",
    "MapleMono-CN-SemiBold.ttf": "320c87dc4134f0bc88ad878255af1f1957c9e7898e76638efc9a3cdf3868574e",
}

CJK_RANGES = (
    (0x1100, 0x11FF),  # Hangul Jamo
    (0x2E80, 0x2FFF),  # radicals, Kangxi, ideographic description
    (0x3000, 0x33FF),  # CJK punctuation, kana, bopomofo, enclosed and compatibility
    (0x3400, 0x4DBF),  # Han extension A
    (0x4E00, 0x9FFF),  # Han
    (0xA960, 0xA97F),  # Hangul Jamo extended A
    (0xAC00, 0xD7FF),  # Hangul syllables and Jamo extended B
    (0xF900, 0xFAFF),  # Han compatibility
    (0xFE10, 0xFE1F),  # vertical forms
    (0xFE30, 0xFE4F),  # CJK compatibility forms
    (0xFF00, 0xFFEF),  # halfwidth and fullwidth forms
    (0x20000, 0x3FFFF),  # Han extensions B and later
)


# The terminal formatter (pinnedCellClass in jni_bridge.cpp) draws these as
# plain text runs, so every codepoint here must come from this face.
PLAIN_RANGES = (
    (0x2500, 0x259F),  # box drawing, blocks
    (0x2800, 0x28FF),  # braille
    (0xE0A0, 0xE0A2),  # Powerline branch, line number, lock
    (0xE0B0, 0xE0B7),  # Powerline arrows and semicircles
)


def is_cjk(codepoint: int) -> bool:
    return any(low <= codepoint <= high for low, high in CJK_RANGES)


def subset_font(source: Path, output: Path) -> None:
    font = TTFont(source)
    unicodes = [c for c in font.getBestCmap() if not is_cjk(c)]
    options = subset.Options()
    options.layout_features = ["*"]
    options.name_IDs = ["*"]
    options.name_languages = ["*"]
    options.name_legacy = True
    options.notdef_outline = True
    options.glyph_names = True
    subsetter = subset.Subsetter(options)
    subsetter.populate(unicodes=unicodes)
    subsetter.subset(font)
    font.save(output)


def verify(source: Path, output: Path) -> None:
    before, after = TTFont(source), TTFont(output)
    for table, fields in {
        "head": ["unitsPerEm"],
        "hhea": ["ascent", "descent", "lineGap"],
        "OS/2": ["sTypoAscender", "sTypoDescender", "sTypoLineGap", "usWinAscent", "usWinDescent"],
    }.items():
        for field in fields:
            assert getattr(before[table], field) == getattr(after[table], field), (table, field)
    for name_id in (1, 2, 4, 6, 13, 14):
        assert str(before["name"].getName(name_id, 3, 1, 0x409)) == str(
            after["name"].getName(name_id, 3, 1, 0x409)
        ), name_id
    old_cmap, new_cmap = before.getBestCmap(), after.getBestCmap()
    assert set(new_cmap) == {c for c in old_cmap if not is_cjk(c)}, "kept set is not upstream minus CJK"
    for codepoint, glyph in new_cmap.items():
        assert after["hmtx"][glyph][0] == before["hmtx"][old_cmap[codepoint]][0], hex(codepoint)
    for low, high in PLAIN_RANGES:
        for codepoint in range(low, high + 1):
            assert codepoint in new_cmap, hex(codepoint)
    for codepoint in (0x23BF, 0x25CF, 0x276F):
        assert codepoint in new_cmap, hex(codepoint)
    print(
        f"{output.name}: {len(old_cmap)} -> {len(new_cmap)} codepoints, "
        f"{source.stat().st_size} -> {output.stat().st_size} bytes"
    )


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    source_dir, output_dir = Path(sys.argv[1]), Path(sys.argv[2])
    for name, expected in SOURCES.items():
        source = source_dir / name
        actual = hashlib.sha256(source.read_bytes()).hexdigest()
        if actual != expected:
            sys.exit(f"{source}: SHA-256 {actual} is not the pinned upstream v7.900 file")
        output = output_dir / name
        subset_font(source, output)
        verify(source, output)


if __name__ == "__main__":
    main()
