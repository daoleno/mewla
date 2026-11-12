#!/usr/bin/env python3
"""The corpus shots' pixel check: rows of block and line glyphs must touch.

    check.py <shots dir>     # box.png from android.sh, web.ts or the iOS test (MEWLA_TEST_OUT/corpus)

The box screen (gen.py) has a wall three rows tall at rows 12-14: eight full blocks (cols 0-7), eight
light verticals (9-16) and eight heavy verticals (18-25). A renderer whose cells are not whole pixels,
or whose line glyphs come from a font shorter than the row, leaves a seam between the rows: a pixel of
background inside the wall. The cell size is the image's size over the screen's grid, so any scale works.
Exits 1 on a seam. Needs Pillow (uv run --with pillow).
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
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
