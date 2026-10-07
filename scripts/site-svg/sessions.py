# One Session, two views: structured Chat and the live Terminal, side by side.
from common import Svg, chevron, client_badge, dots_icon, phone, round_button, state, tw

TITLE = "Sync fix"


def header(s, sx, sy, sw, view):
    y = sy + 60
    round_button(s, sx + 30, y, 17)
    chevron(s, sx + 27, y, 6, stroke="ink", direction="left")
    pw = sw - 152
    s.rect(sx + 54, y - 21, pw, 42, rx=21, fill="card", stroke="line")
    client_badge(s, sx + 76, y, "codex", r=12)
    s.text(sx + 96, y - 3, TITLE, size=14, weight=600)
    s.text(sx + 96, y + 13, f"atlas-notes · {view}", size=11, fill="soft")
    # Git and ⋯ share one pill.
    gx = sx + sw - 88
    s.rect(gx, y - 21, 76, 42, rx=21, fill="card", stroke="line")
    s.circle(gx + 20, y - 7, 2.6, stroke="ink", sw=1.4)
    s.circle(gx + 20, y + 8, 2.6, stroke="ink", sw=1.4)
    s.circle(gx + 30, y - 4, 2.6, stroke="ink", sw=1.4)
    s.path(f"M{gx + 20} {y - 4.4}v9.8M{gx + 30} {y - 1.4}c0 5-10 3-10 7", stroke="ink", sw=1.4)
    dots_icon(s, gx + 54, y)
    return y + 34


def tool(s, x, y, w, glyph, title, detail=None, st=None, word=None):
    s.rect(x, y - 12, 14, 14, rx=3, stroke="soft", sw=1.2)
    s.text(x + 7, y - 1.5, glyph, size=9, fill="soft", anchor="middle", mono=True, weight=600)
    s.text(x + 24, y, title, size=13, fill="soft")
    if detail:
        s.text(x + 24 + tw(title, 13) + 6, y, detail, size=12, fill="faint", mono=True)
    if st:
        state(s, st, x + w, y, size=12, word=word)


def chat(s, sx, sy, sw, sh):
    y = header(s, sx, sy, sw, "Chat")
    s.rect(sx + sw / 2 - 26, y + 6, 52, 22, rx=11, stroke="line")
    s.text(sx + sw / 2, y + 21, "Today", size=11, fill="soft", anchor="middle")
    y += 46
    msg = ["Fix the sync conflict bug.", "Keep both copies when edits collide."]
    bw = 248
    s.rect(sx + sw - 16 - bw, y, bw, 58, rx=18, fill="me")
    for i, ln in enumerate(msg):
        s.text(sx + sw - bw, y + 24 + i * 20, ln, size=13.5)
    y += 90
    x, w = sx + 20, sw - 40
    tool(s, x, y, w, "R", "Read", "sync/merge.go")
    y += 30
    tool(s, x, y, w, "E", "Edited 2 files", "+35 −12")
    y += 30
    tool(s, x, y, w, "$", "Ran go test", None, "failed", "exit 1")
    s.rect(x + 24, y + 10, w - 24, 44, rx=10, fill="tint")
    s.text(x + 36, y + 29, "--- FAIL: TestMergeKeepsBoth", size=11, mono=True)
    s.text(x + 36, y + 45, "merge_test.go:88: want 2 copies", size=11, fill="soft", mono=True)
    y += 82
    tool(s, x, y, w, "E", "Edited sync/merge.go", "+4 −1")
    y += 30
    tool(s, x, y, w, "$", "Ran go test", None, "ready", "Passed")
    y += 40
    for i, ln in enumerate(["Conflicting edits now keep both copies;",
                            "the older one is named “conflicted copy”.",
                            "All 41 sync tests pass."]):
        s.text(x, y + i * 22, ln, size=14)
    # Composer
    cy = sy + sh - 74
    s.rect(sx + 14, cy, sw - 28, 48, rx=24, fill="card", stroke="line")
    s.circle(sx + 40, cy + 24, 15, fill="tint")
    s.path(f"M{sx + 40} {cy + 18}v12M{sx + 34} {cy + 24}h12", stroke="ink", sw=1.5, extra=' stroke-linecap="round"')
    s.text(sx + 64, cy + 29, "Message the agent", size=14, fill="faint")
    s.circle(sx + sw - 38, cy + 24, 15, fill="tint")
    s.path(f"M{sx + sw - 38} {cy + 30}v-12M{sx + sw - 43} {cy + 23}l5-5 5 5", stroke="soft", sw=1.6,
           extra=' stroke-linecap="round" stroke-linejoin="round"')


def terminal(s, sx, sy, sw, sh):
    y = header(s, sx, sy, sw, "Terminal")
    lines = [
        ("$ codex", "ink"),
        ("› Fix the sync conflict bug. Keep", "ink"),
        ("  both copies when edits collide.", "ink"),
        ("", "ink"),
        ("• Read sync/merge.go", "soft"),
        ("• Edited 2 files (+35 -12)", "soft"),
        ("• Ran go test ./sync/...", "soft"),
        ("  --- FAIL: TestMergeKeepsBoth", "failed"),
        ("• Edited sync/merge.go (+4 -1)", "soft"),
        ("• Ran go test ./sync/...", "soft"),
        ("  ok   sync   41 passed  2.1s", "ready"),
        ("", "ink"),
        ("Conflicting edits now keep both", "ink"),
        ("copies; the older one is named", "ink"),
        ("“conflicted copy”. 41 tests pass.", "ink"),
        ("", "ink"),
        ("›", "ink"),
    ]
    y += 12
    for i, (ln, col) in enumerate(lines):
        s.text(sx + 16, y + i * 21, ln, size=12.5, fill=col, mono=True)
    s.rect(sx + 30, y + (len(lines) - 1) * 21 - 12, 8, 15, fill="ink")
    # Key bar
    ky = sy + sh - 66
    s.line(sx, ky - 10, sx + sw, ky - 10, stroke="line")
    keys = ["esc", "tab", "ctrl", "↑", "↓", "←", "→", "/"]
    kx = sx + 12
    for k in keys:
        kw = 22 + len(k) * 6
        s.rect(kx, ky, kw, 30, rx=8, fill="card", stroke="line")
        s.text(kx + kw / 2, ky + 20, k, size=12, fill="soft", anchor="middle", mono=True)
        kx += kw + 6


def build():
    W, H = 800, 780
    s = Svg("sp", W, H, "The same Session as Chat and as the live Terminal",
            "Left: a codex Session in Chat, with your message, tool rows that show a failed test and then a pass, "
            "and the agent's reply. Right: the same Session as the live terminal on paper, with the on-screen key bar.")
    for i, fn in enumerate((chat, terminal)):
        sx, sy, sw, sh = phone(s, 20 + i * 390, 20, 370, H - 40)
        fn(s, sx, sy, sw, sh)
    return s.render()


if __name__ == "__main__":
    import sys
    sys.stdout.write(build())
