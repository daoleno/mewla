#!/usr/bin/env python3
"""The corpus shots' pixel checks: rows of block and line glyphs must touch; narrow emoji and flags
are drawn at full size.

    check.py <shots dir>     # box.png from android.sh, web.ts or the iOS test (MEWLA_TEST_OUT/corpus)

The box screen (gen.py) has a wall three rows tall at rows 12-14: eight full blocks (cols 0-7), eight
light verticals (9-16) and eight heavy verticals (18-25). A renderer whose cells are not whole pixels,
or whose line glyphs come from a font shorter than the row, leaves a seam between the rows: a pixel of
background inside the wall. The cell size is the image's size over the screen's grid, so any scale works.
The emoji screen (gen.py, when emoji.png is there): the VS16 heart at row 0 col 7 has a blank cell
after it, so it is drawn at full size and inks col 8; the text heart at row 3 col 6 (no VS16) stays in
its cell, so col 7 is background; the two regional indicators at row 2 cols 7-10 are one flag, so
those cells carry its colors (saturated pixels), not two letters.
Exits 1 on a failure. Needs Pillow (uv run --with pillow).
"""
import json
import pathlib
import sys

from PIL import Image

WALL_TOP, WALL_ROWS = 12, 3


def near(a, b, tolerance=24):
    return all(abs(x - y) <= tolerance for x, y in zip(a[:3], b[:3]))


def main() -> int:
    shots = pathlib.Path(sys.argv[1])
    corpus = json.loads((pathlib.Path(__file__).parent / "corpus.json").read_text())
    cols, rows = corpus["box"]["cols"], corpus["box"]["rows"]
    image = Image.open(shots / "box.png").convert("RGB")
    W, H = image.size
    cw, ch = W / cols, H / rows
    px = image.load()
    background = px[2, H - 2]
    y0, y1 = round(WALL_TOP * ch) + 1, round((WALL_TOP + WALL_ROWS) * ch) - 2
    failures = []

    # The full blocks: every pixel inside the wall is ink.
    holes = sum(1 for y in range(y0, y1 + 1) for x in range(round(0.5 * cw), round(7.5 * cw))
                if near(px[x, y], background))
    if holes:
        failures.append(f"full blocks: {holes} background pixels inside the wall")

    # The verticals: the column each line is drawn at (its most inked x in the first row's middle)
    # is ink the whole way down.
    for name, first in (("light verticals", 9), ("heavy verticals", 18)):
        for col in range(first, first + 8):
            xs = range(round(col * cw), round((col + 1) * cw))
            ym = round((WALL_TOP + 0.5) * ch)
            x = max(xs, key=lambda x: sum(abs(a - b) for a, b in zip(px[x, ym], background)))
            gaps = sum(1 for y in range(y0, y1 + 1) if near(px[x, y], background))
            if gaps:
                failures.append(f"{name}: col {col} (x {x}) has {gaps} background pixels between rows")
                break

    print(f"{shots / 'box.png'}: cell {cw:.2f}x{ch:.2f} px, wall y {y0}-{y1}: " + ("seams: " + "; ".join(failures) if failures else "rows touch"))
    if (shots / "emoji.png").exists():
        failures += emoji(shots / "emoji.png", corpus["emoji"]["cols"], corpus["emoji"]["rows"])
    return 1 if failures else 0


def emoji(path, cols, rows):
    image = Image.open(path).convert("RGB")
    W, H = image.size
    cw, ch = W / cols, H / rows
    px = image.load()
    background = px[2, H - 2]

    def cells(row, c0, c1, test):
        return sum(1 for y in range(round(row * ch) + 1, round((row + 1) * ch) - 1)
                   for x in range(round(c0 * cw) + 1, round((c1 + 1) * cw) - 1) if test(px[x, y]))

    ink = lambda p: not near(p, background, 40)
    saturated = lambda p: max(p) - min(p) > 80
    spill = cells(0, 8, 8, ink)
    stays = cells(3, 7, 7, ink)
    flag = cells(2, 7, 10, saturated)
    failures = []
    if spill < 4:
        failures.append(f"the VS16 heart does not reach the blank cell after it ({spill} inked pixels)")
    if stays > 0:
        failures.append(f"the text heart spills into the next cell ({stays} inked pixels)")
    if flag < 20:
        failures.append(f"no flag across the regional indicators ({flag} colored pixels)")
    print(f"{path}: emoji spill {spill} px, text heart spill {stays} px, flag {flag} colored px: " + ("; ".join(failures) if failures else "ok"))
    return failures


if __name__ == "__main__":
    sys.exit(main())
