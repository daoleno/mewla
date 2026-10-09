#!/usr/bin/env python3
# Writes the documentation drawings to docs/assets/ (README and docs pages).
# Run from anywhere: python3 scripts/site-svg/build.py  (needs Pillow for the pet)
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
OUT = os.path.join(HERE, "..", "..", "docs", "assets")

import brain  # noqa: E402
import hero  # noqa: E402
import modules  # noqa: E402
import sessions  # noqa: E402
from common import write  # noqa: E402

FILES = {
    "hero.svg": hero.build,
    "sessions-pair.svg": sessions.build,
    "brain.svg": brain.build,
    **{f"{k}.svg": f for k, f in modules.ALL.items()},
}

if __name__ == "__main__":
    os.makedirs(OUT, exist_ok=True)
    for name, fn in FILES.items():
        write(os.path.join(OUT, name), fn())
        print(name)
