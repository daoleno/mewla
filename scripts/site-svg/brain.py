# Brain on a wide screen: the docked menu, the conversation with its goal
# line, folded steps and Work slips, and the Work column with the cat perched
# on the slip that needs you. The atlas-notes story matches the homepage.
import json
import os
import re

from common import C, Svg, chevron, mark, seal, slip

GOAL = "Ship atlas-notes v1.4 this week"


HERE = os.path.dirname(os.path.abspath(__file__))
ICONS = os.path.join(HERE, "..", "..", "app", "components", "icons")
GLYPHS = {
    name: json.loads(paths)
    for name, paths in re.findall(r'^  "([a-z-]+)": (\[.*\]),$', open(os.path.join(ICONS, "phosphorGlyphs.ts")).read(), re.M)
}
BRAIN_GLYPH = re.search(r'brain: \{\s*strokes: \[\s*"([^"]+)"', open(os.path.join(ICONS, "mewlaGlyphs.ts")).read()).group(1)
MENU = (("brain", "Brain"), ("sessions", "Sessions"), ("calendar", "Calendar"), ("plugins", "Plugins"),
        ("skills", "Skills"), ("stats", "Stats"), ("resources", "Resources"))


def icon(s, name, x, y, size=18, ink="soft"):
    """The app's own glyphs (components/icons), top-left at (x, y)."""
    g = f'<g transform="translate({x:g} {y:g}) scale({size / 256:g})">'
    if name == "brain":
        g += f'<path d="{BRAIN_GLYPH}" fill="none" stroke="{C[ink]}" stroke-width="24" stroke-linecap="round" stroke-linejoin="round"/>'
        g += "".join(f'<circle cx="{cx}" cy="142" r="14" fill="{C[ink]}"/>' for cx in (92, 128, 164))
    else:
        g += "".join(f'<path d="{d}" fill="{C[ink]}"/>' for d in GLYPHS[name])
    s.add(g + "</g>")


def sidebar(s, x, y, w, h):
    """One list (Brain, Sessions, the tools), Settings at the bottom, then the server line."""
    s.line(x + w, y, x + w, y + h, stroke="line")
    seal(s, x + 20, y + 22, 26)
    s.text(x + 54, y + 41, "Mewla", size=21, weight=800, display=True)
    ry = y + 70
    for ic, label in MENU:
        on = ic == "brain"
        if on:
            s.rect(x + 10, ry, w - 20, 38, rx=10, fill="select")
        icon(s, ic, x + 22, ry + 10, ink="ink" if on else "soft")
        s.text(x + 52, ry + 24, label, size=14.5, weight=500)
        ry += 42
    by = y + h - 92
    icon(s, "settings", x + 22, by + 10)
    s.text(x + 52, by + 24, "Settings", size=14.5, weight=500)
    s.line(x + 10, by + 50, x + w - 10, by + 50, stroke="line")
    icon(s, "desktop", x + 24, by + 63, size=14, ink="soft2")
    s.text(x + 52, by + 74, "Studio Mac", size=12.5, fill="soft", weight=500)
    s.text(x + w - 14, by + 74, "Connected", size=12, fill="soft2", anchor="end")


def conversation(s, x, y, w, h):
    s.text(x + 24, y + 42, "Brain", size=26, weight=800, display=True)
    s.add(f'<text x="{x + 24:g}" y="{y + 70:g}" font-size="14" fill="{C["ink"]}" font-weight="600">{GOAL}'
          f'<tspan font-weight="400" fill="{C["soft"]}"> · 2 of 5 back</tspan></text>')
    # Your message
    msg = ["Ship atlas-notes v1.4 this week: fix the sync bug,", "tidy the settings copy, write release notes."]
    bw = 360
    s.rect(x + w - 24 - bw, y + 90, bw, 58, rx=18, fill="me")
    for i, ln in enumerate(msg):
        s.text(x + w - 24 - bw + 16, y + 114 + i * 20, ln, size=13.5)
    # Folded tool rows
    ry = y + 176
    s.rect(x + 24, ry - 13, 14, 14, rx=3, stroke="soft", sw=1.3)
    s.path(f"M{x + 28} {ry - 6}l2 2 4-4", stroke="soft", sw=1.3, extra=' stroke-linecap="round"')
    s.text(x + 48, ry, "Worked · 6 steps", size=13.5, fill="soft")
    chevron(s, x + w - 32, ry - 5, 5)
    s.text(x + 24, ry + 32, "On it. Five parts, each tracked as Work. I'll only call you when it matters.", size=14.5)
    sy = ry + 52
    sw_ = w - 48
    sy += slip(s, x + 24, sy, sw_, "Codex · atlas-notes · Yesterday", "Conflict wording", "Five apps compared. “Conflicted copy” wins 3 of 5.", "ready", dot=True) + 12
    sy += slip(s, x + 24, sy, sw_, "Claude Code · atlas-notes · Yesterday", "Sync tests", "41 pass, but 2 were flaky and needed a retry.", "warning") + 12
    slip(s, x + 24, sy, sw_, "Pi · atlas-notes · Yesterday", "Post notes to Notion", "Notion said 401: the token expired. Nothing was posted.", "failed")
    # Composer
    cy = y + h - 64
    s.rect(x + 18, cy, w - 36, 48, rx=24, fill="card", stroke="line")
    s.circle(x + 44, cy + 24, 15, fill="tint")
    s.path(f"M{x + 44} {cy + 18}v12M{x + 38} {cy + 24}h12", stroke="ink", sw=1.5, extra=' stroke-linecap="round"')
    s.text(x + 70, cy + 29, "Tell Brain…", size=14.5, fill="faint")
    s.circle(x + w - 42, cy + 24, 15, fill="tint")
    s.path(f"M{x + w - 42} {cy + 30}v-12M{x + w - 47} {cy + 23}l5-5 5 5", stroke="soft", sw=1.5, extra=' stroke-linecap="round" stroke-linejoin="round"')


def work_column(s, x, y, w, h):
    s.line(x, y, x, y + h, stroke="line")
    x += 22
    w -= 44
    s.text(x, y + 44, "Work", size=15, weight=600)
    s.text(x, y + 64, "1 needs you · 1 running · 1 back · 1 waiting", size=12, fill="soft")
    gy = y + 96
    s.text(x, gy, "Needs you · 1", size=12, fill="soft")
    gy += 46
    gy += slip(s, x, gy, w, "00:44", "Sync fix needs your call", "Keep both copies, or the newest edit?", "needs", perch=True) + 26
    s.text(x, gy, "Running · 1", size=12, fill="soft")
    gy += 12
    gy += slip(s, x, gy, w, "Claude Code · 00:47", "Tidy the settings copy", "Rewriting strings · 7 of 12", "running") + 26
    s.text(x, gy, "Back · 1", size=12, fill="soft")
    gy += 12
    gy += slip(s, x, gy, w, "00:30", "Conflict wording", None, "ready", dot=True) + 26
    s.text(x, gy, "Waiting · 1", size=12, fill="soft")
    gy += 12
    slip(s, x, gy, w, "00:18", "Draft the release notes", None, "waiting")


def build():
    W, H = 1200, 700
    s = Svg("bw", W, H,
            "Brain with its Work column",
            "Brain on a wide screen. The menu is docked on the left. The conversation shows the goal line "
            "'Ship atlas-notes v1.4 this week, 2 of 5 back', your message, a folded 'Worked, 6 steps' row and "
            "three Work slips: Ready, Needs review and Failed. The Work column on the right groups Work as "
            "Needs you, Running, Back and Waiting; the cat perches on the slip that needs you.")
    wx, wy, ww, wh = 20, 20, W - 40, H - 40
    s.rect(wx, wy, ww, wh, rx=16, fill="paper", stroke="border")
    sidebar(s, wx, wy, 210, wh)
    work_column(s, wx + ww - 340, wy, 340, wh)
    conversation(s, wx + 210, wy, ww - 210 - 340, wh)
    return s.render()


if __name__ == "__main__":
    import sys
    sys.stdout.write(build())
