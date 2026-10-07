# Brain on a wide screen: the docked menu, the conversation with its goal
# line, folded steps and Work slips, and the Work column with the cat perched
# on the slip that needs you. The atlas-notes story matches the homepage.
from common import C, Svg, chevron, mark, seal, slip

GOAL = "Ship atlas-notes v1.4 this week"


def icon(s, name, x, y):
    """Soft-ink menu glyphs, 18 px, top-left at (x, y)."""
    st = ' stroke-linecap="round" stroke-linejoin="round"'
    if name == "brain":
        s.path(f"M{x + 2} {y + 9}a7 7 0 1 1 3 5.7L{x + 2} {y + 16}l1-3.6A7 7 0 0 1 {x + 2} {y + 9}Z", stroke="soft", sw=1.4, extra=st)
    elif name == "sessions":
        s.rect(x + 1, y + 2, 16, 14, rx=2.5, stroke="soft", sw=1.4)
        s.path(f"M{x + 5} {y + 7}l2.5 2-2.5 2M{x + 9.5} {y + 12}h3", stroke="soft", sw=1.4, extra=st)
    elif name == "calendar":
        s.rect(x + 1, y + 3, 16, 14, rx=2.5, stroke="soft", sw=1.4)
        s.path(f"M{x + 1} {y + 7.5}h16M{x + 5} {y + 1}v3M{x + 13} {y + 1}v3", stroke="soft", sw=1.4, extra=st)
    elif name == "plugins":
        s.path(f"M{x + 6} {y + 1}v4M{x + 12} {y + 1}v4M{x + 4} {y + 5}h10v4a5 5 0 0 1-10 0ZM{x + 9} {y + 14}v4", stroke="soft", sw=1.4, extra=st)
    elif name == "skills":
        s.path(f"M{x + 9} {y + 2}l8 4-8 4-8-4ZM{x + 1} {y + 10}l8 4 8-4M{x + 1} {y + 14}l8 4 8-4", stroke="soft", sw=1.3, extra=st)
    elif name == "stats":
        s.path(f"M{x + 4} {y + 16}v-5M{x + 9} {y + 16}v-10M{x + 14} {y + 16}v-7", stroke="soft", sw=1.6, extra=st)
    elif name == "resources":
        s.rect(x + 1, y + 2, 16, 14, rx=2.5, stroke="soft", sw=1.4)
        s.path(f"M{x + 3} {y + 10}h3l2-4 2.5 7 2-3h2.5", stroke="soft", sw=1.3, extra=st)
    elif name == "settings":
        s.path(f"M{x + 1} {y + 5}h16M{x + 1} {y + 13}h16", stroke="soft", sw=1.4, extra=st)
        s.circle(x + 6, y + 5, 2.4, fill="paper", stroke="soft", sw=1.4)
        s.circle(x + 12, y + 13, 2.4, fill="paper", stroke="soft", sw=1.4)


def sidebar(s, x, y, w, h):
    s.line(x + w, y, x + w, y + h, stroke="line")
    seal(s, x + 20, y + 22, 26)
    s.text(x + 54, y + 41, "Mewla", size=21, weight=800, display=True)
    rows = [("brain", "Brain", True), ("sessions", "Sessions", False)]
    ry = y + 70
    for ic, label, on in rows:
        if on:
            s.rect(x + 10, ry, w - 20, 38, rx=10, fill="select")
        icon(s, ic, x + 22, ry + 10)
        s.text(x + 52, ry + 24, label, size=14.5, weight=500 if on else 400)
        ry += 42
    s.rect(x + 10, ry + 4, w - 20, 46, rx=10, fill="card", stroke="line")
    s.text(x + 24, ry + 24, "Studio Mac", size=13.5, weight=500)
    mark(s, "ready", x + 30, ry + 37, r=4.5)
    s.text(x + 40, ry + 41, "Connected", size=11.5, fill="soft")
    ry += 72
    s.text(x + 22, ry, "On this computer", size=11.5, fill="soft2")
    ry += 14
    for ic, label in (("calendar", "Calendar"), ("plugins", "Plugins"), ("skills", "Skills"), ("stats", "Stats"), ("resources", "Resources")):
        icon(s, ic, x + 22, ry + 8)
        s.text(x + 52, ry + 22, label, size=14.5)
        ry += 36
    ry += 22
    s.text(x + 22, ry, "App", size=11.5, fill="soft2")
    icon(s, "settings", x + 22, ry + 22)
    s.text(x + 52, ry + 36, "Settings", size=14.5)


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
