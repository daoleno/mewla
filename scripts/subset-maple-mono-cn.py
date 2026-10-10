#!/usr/bin/env python3
"""Subset Maple Mono CN for the native app.

Keeps every glyph of the upstream font except Han ideographs outside GB2312
and Big5 level 1, the common simplified and traditional sets. Everything a
terminal draws (Latin, box drawing, blocks, braille, arrows, dingbats,
Powerline/Nerd icons, CJK punctuation, kana, fullwidth forms) stays, as do
the name table, metrics, hinting and layout features, so the face keeps its
PostScript name and 2:1 cell width.

Usage (from the repository root, with fontTools installed):

  python3 scripts/subset-maple-mono-cn.py <dir with upstream TTFs> app/assets/fonts

The input is the upstream Maple Mono CN v7.900 release files
`MapleMono-CN-{Regular,SemiBold}.ttf`, pinned by SHA-256 below. Git history
before the subset commit holds the same files.
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


def is_han(codepoint: int) -> bool:
    return (
        0x3400 <= codepoint <= 0x4DBF
        or 0x4E00 <= codepoint <= 0x9FFF
        or 0xF900 <= codepoint <= 0xFAFF
        or 0x20000 <= codepoint <= 0x3FFFF
    )


def double_byte_charset(codec: str, leads: range, trails: list[int]) -> set[int]:
    chars: set[int] = set()
    for lead in leads:
        for trail in trails:
            try:
                text = bytes([lead, trail]).decode(codec)
            except UnicodeDecodeError:
                continue
            chars.update(ord(ch) for ch in text)
    return chars


def kept_han() -> set[int]:
    gb2312 = double_byte_charset("gb2312", range(0xA1, 0xF8), list(range(0xA1, 0xFF)))
    big5_level1 = double_byte_charset(
        "big5", range(0xA4, 0xC7), list(range(0x40, 0x7F)) + list(range(0xA1, 0xFF))
    )
    return {c for c in gb2312 | big5_level1 if is_han(c)}


def subset_font(source: Path, output: Path, keep_han: set[int]) -> None:
    font = TTFont(source)
    unicodes = [c for c in font.getBestCmap() if not is_han(c) or c in keep_han]
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
    dropped = set(old_cmap) - set(new_cmap)
    assert all(is_han(c) for c in dropped), "dropped a non-Han codepoint"
    cell = after["hmtx"][new_cmap[ord("a")]][0]
    for codepoint, glyph in new_cmap.items():
        advance = after["hmtx"][glyph][0]
        assert advance == before["hmtx"][old_cmap[codepoint]][0], hex(codepoint)
        if is_han(codepoint) or 0xFF01 <= codepoint <= 0xFF5E:
            assert advance == 2 * cell, (hex(codepoint), advance, cell)
    for codepoint in (0x2500, 0x2588, 0x23BF, 0x2800, 0x28FF, 0xE0B0, 0x3001, 0x4E2D):
        assert codepoint in new_cmap, hex(codepoint)
    print(
        f"{output.name}: {len(old_cmap)} -> {len(new_cmap)} codepoints, "
        f"{source.stat().st_size} -> {output.stat().st_size} bytes"
    )


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    source_dir, output_dir = Path(sys.argv[1]), Path(sys.argv[2])
    keep_han = kept_han()
    for name, expected in SOURCES.items():
        source = source_dir / name
        actual = hashlib.sha256(source.read_bytes()).hexdigest()
        if actual != expected:
            sys.exit(f"{source}: SHA-256 {actual} is not the pinned upstream v7.900 file")
        output = output_dir / name
        subset_font(source, output, keep_han)
        verify(source, output)


if __name__ == "__main__":
    main()
