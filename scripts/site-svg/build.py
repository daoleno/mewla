#!/usr/bin/env python3
# Writes the README drawings under site/assets/. The homepage no longer uses
# them; its ensō comes from enso.py.
# Run from anywhere: python3 scripts/site-svg/build.py
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
OUT = os.path.join(HERE, "..", "..", "site", "assets")

import brain  # noqa: E402
import hero  # noqa: E402
import modules  # noqa: E402
import sessions  # noqa: E402
from common import write  # noqa: E402


def sessions_pair():
    """Chat and Terminal side by side, for the README where nothing is clickable."""
    inner = sessions.build()
    inner = re.sub(r"<title.*?</desc>", "", inner)
    inner = inner.replace(' role="img" aria-labelledby="ss-t ss-d"', ' aria-hidden="true"')
    a = inner.replace("<svg ", '<svg x="0" data-state="2" ', 1)
    b = inner.replace("<svg ", '<svg x="400" data-state="3" ', 1)
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 780 760" width="780" height="760" role="img" aria-labelledby="sp-t sp-d">'
            '<title id="sp-t">The same Session as Chat and as the live Terminal</title>'
            '<desc id="sp-d">Left: a claude Session as Chat with the request, tool rows, reply and plan. Right: the same Session as the live terminal grid.</desc>'
            + a + b + "</svg>\n")


FILES = {
    "hero.svg": lambda: hero.build(False),
    "sessions-pair.svg": sessions_pair,
    # The README is static, so show the Delegate state.
    "brain.svg": lambda: brain.build(False).replace("<svg ", '<svg data-state="2" ', 1),
    **{f"{k}.svg": f for k, f in modules.ALL.items()},
}

if __name__ == "__main__":
    for name, fn in FILES.items():
        write(os.path.join(OUT, name), fn())
        print(name)
