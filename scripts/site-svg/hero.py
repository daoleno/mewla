# Hero: every Session on the host converges into one phone.
from common import C, Svg, phone, dot

SESSIONS = [
    ("claude", "onboarding", "run"),
    ("codex", "session-auth", "need"),
    ("pi", "changelog", "done"),
    ("shell", "~/mewla", "run"),
    ("claude", "ios-crash", "run"),
    ("grok", "parser-port", "run"),
    ("opencode", "docs-sync", "done"),
    ("cursor", "flaky-tests", "run"),
    ("codex", "deps-bump", "done"),
    ("brain", "Brain", "run"),
]
STATUS = {"run": "running", "need": "needs you", "done": "done"}

CSS = """
.{k} .lp{{stroke:#34352d;transition:stroke .25s}}
.{k} .lane.run .lp{{stroke:#3c4a40}}
.{k} .lane.need .lp{{stroke:#5a3a2c}}
.{k} .pu{{stroke-dasharray:46 1000;stroke-dashoffset:1046;animation:{k}-run var(--d,7s) linear infinite;animation-delay:var(--o,0s)}}
.{k} .need .pu{{stroke:#f0895c}}
.{k} .blink{{animation:{k}-blink 2.4s ease-in-out infinite}}
.{k} .hit{{stroke:transparent;stroke-width:22;pointer-events:stroke}}
.{k} .rowbg{{transition:fill .25s}}
.{k} .lane:hover .lp{{stroke:#8fcfa6}}
.{k} .lane.need:hover .lp{{stroke:#f0895c}}
.{k} .lane:hover .rowbg{{fill:#22231d}}
@keyframes {k}-run{{to{{stroke-dashoffset:0}}}}
@keyframes {k}-blink{{50%{{opacity:.25}}}}
@media (prefers-reduced-motion:reduce){{.{k} .pu{{display:none}}.{k} .blink{{animation:none}}}}
"""


def build(narrow=False):
    k = "hn" if narrow else "hw"
    W, H = (400, 540) if narrow else (1200, 760)
    desc = ("Five Sessions on your own computer: agents on claude, codex and grok, a plain shell and Brain. "
            "Each one runs into the phone's Sessions list. Four are running and one needs you.") if narrow else (
            "Ten Sessions on your own computer: agents on claude, codex, pi, grok, opencode and cursor, a plain shell and Brain. "
            "Each one runs into the phone's Sessions list. Most are running, three are done and one needs you.")
    s = Svg(k, W, H, "Every Session on your computer, in one phone", desc,
            css=CSS.format(k=k))

    # Labels across the top, two rows when narrow.
    sessions = [SESSIONS[i] for i in (0, 1, 3, 5, 9)] if narrow else SESSIONS
    if narrow:
        pos = [(16 + i * 78, 20) for i in range(len(sessions))]
        px, py, pw, ph = 105, 250, 190, 400
    else:
        pos = [(36 + i * 114, 22) for i in range(len(SESSIONS))]
        px, py, pw, ph = 880, 318, 250, 520

    # Faint construction grid, behind everything.
    if not narrow:
        for gx in range(0, W + 1, 120):
            s.line(gx, 96, gx, H, stroke="#15160f")
        s.line(0, 96, W, 96, stroke="line")
        s.text(px - 18, py + 40, "YOUR PHONE", size=10, fill="faint", mono=True, anchor="end", extra=' letter-spacing="1.5"')
        s.line(px - 12, py + 37, px - 2, py + 37, stroke="line2")

    n = len(sessions)
    ex0, ex1 = px + pw * 0.2, px + pw * 0.8
    ends = [ex0 + (ex1 - ex0) * i / (n - 1) for i in range(n)]

    sx, sy, sw, sh = None, None, None, None
    # Phone first so lanes sit on top of its frame edge.
    s.g()
    sx, sy, sw, sh = phone(s, px, py, pw, ph)
    s.end()

    rs = 0.76 if narrow else 1.0
    bar_y = sy + 40 * rs
    # App bar: segmented Brain / Sessions.
    seg_w = 120 * rs
    s.rect(sx + sw / 2 - seg_w / 2, bar_y, seg_w, 24 * rs, rx=12 * rs, fill="panel3")
    s.rect(sx + sw / 2, bar_y + 2 * rs, seg_w / 2 - 2 * rs, 20 * rs, rx=10 * rs, fill="#33342c")
    s.text(sx + sw / 2 - seg_w / 4, bar_y + 16 * rs, "Brain", size=11 * rs, fill="dim", anchor="middle")
    s.text(sx + sw / 2 + seg_w / 4, bar_y + 16 * rs, "Sessions", size=11 * rs, fill="ink", anchor="middle", weight=600)
    s.circle(sx + 22 * rs, bar_y + 12 * rs, 11 * rs, fill="panel3")
    s.circle(sx + sw - 22 * rs, bar_y + 12 * rs, 11 * rs, fill="panel3")
    s.text(sx + 16 * rs, bar_y + 46 * rs + 8, "~/mewla", size=10 * rs, fill="faint", mono=True)

    row_y0 = bar_y + 58 * rs + 8
    row_h = 37 * rs

    for i, ((client, name, st), (lx, ly)) in enumerate(zip(sessions, pos)):
        s.g(f"lane {st}")
        # lane
        x0, y0 = lx + 4, ly + 34
        x1, y1 = ends[i], py - 2
        mid = y0 + (y1 - y0) * 0.55
        d = f"M{x0:.1f} {y0:.1f}C{x0:.1f} {mid:.1f},{x1:.1f} {mid - (y1 - y0) * 0.1:.1f},{x1:.1f} {y1:.1f}"
        s.path(d, cls="hit")
        s.path(d, stroke="#34352d", sw=1, cls="lp")
        if st != "done":
            dur = 6 + (i * 1.7) % 5
            off = -(i * 2.3) % dur
            s.path(d, stroke="sage", sw=1.6, cls="pu", extra=f' pathLength="1000" stroke-linecap="round" style="--d:{dur:.1f}s;--o:{off:.1f}s"')
        s.circle(x1, y1, 1.8, fill="line2")
        # label
        dot(s, lx + 4, ly + 6, st, r=3, cls="blink" if st == "need" else "")
        s.text(lx + 13, ly + 10, client, size=10.5 if not narrow else 9.5, fill="faint", mono=True)
        s.text(lx, ly + 27, name, size=13 if not narrow else 11, fill="ink" if st != "done" else "dim", weight=500)
        # phone row
        ry = row_y0 + i * row_h
        if ry + row_h < sy + sh - 6:
            s.rect(sx + 8 * rs, ry, sw - 16 * rs, row_h - 4 * rs, rx=9 * rs, fill="panel2" if st != "need" else "emberD", cls="rowbg")
            dot(s, sx + 20 * rs, ry + (row_h - 4 * rs) / 2, st, r=3.2 * rs, cls="blink" if st == "need" else "")
            s.text(sx + 32 * rs, ry + 14 * rs, name, size=11.5 * rs, fill="ink", weight=500)
            s.text(sx + 32 * rs, ry + 27 * rs, client, size=9.5 * rs, fill="faint", mono=True)
            s.text(sx + sw - 16 * rs, ry + 20 * rs, STATUS[st], size=9.5 * rs,
                   fill={"run": "sage", "need": "ember", "done": "teal"}[st], anchor="end", mono=True)
        s.end()

    return s.render(rx=0)


if __name__ == "__main__":
    import sys
    sys.stdout.write(build("--narrow" in sys.argv))
