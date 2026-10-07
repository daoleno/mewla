# Hero: every Session on your computer runs in tmux and lands in the phone's
# Sessions list, grouped by directory, each with its state mark.
from common import C, Svg, client_badge, dots_icon, mark, menu_icon, phone, round_button

# (client, title, directory, subtitle, state, time, terminal lines)
SESSIONS = [
    ("codex", "Sync fix", "atlas-notes", "Keep both copies, or the newest edit?", "needs", "00:44",
     ["› go test ./sync/...", "ok   sync   41 passed", "? keep both, or newest"]),
    ("claude", "Settings copy", "atlas-notes", "Rewriting strings · 7 of 12", "running", "00:47",
     ["Rewriting 12 strings", "“Keep both copies”", "7 of 12 done"]),
    ("grok", "Conflict wording", "atlas-notes", "Five apps compared", "ready", "00:30",
     ["Reading 5 apps’ docs", "“conflicted copy” × 3", "Summary ready"]),
    ("pi", "Release notes", "atlas-notes", "Waiting on the sync fix", "waiting", "00:18",
     ["Waiting for merges", "…"]),
    ("shell", "~/atlas-notes", "atlas-notes", "git log --oneline -3", None, "00:12",
     ["$ git log --oneline -3", "a41c2e Fix sync merge", "$ ▍"]),
    ("opencode", "API review", "weather-kit", "3 notes, no blockers", "ready", "Tue",
     ["Reviewing /v2/forecast", "3 notes, 0 blockers"]),
]
NAMES = {"codex": "codex", "claude": "claude", "grok": "grok", "pi": "pi", "shell": "shell", "opencode": "opencode"}

CSS = """
.{k} .flow{{stroke-dasharray:3 6;animation:{k}-f 1.8s linear infinite}}
@keyframes {k}-f{{to{{stroke-dashoffset:-18}}}}
@media (prefers-reduced-motion:reduce){{.{k} .flow{{animation:none}}}}
"""


def build():
    k = "hw"
    W, H = 1200, 700
    s = Svg(k, W, H, "Every Session on your computer, in one phone",
            "Six Sessions run in tmux on your computer: codex, claude, grok and pi agents, a plain shell and opencode. "
            "Each lands in the phone's Sessions list, grouped by directory. One needs you, one is working, "
            "two are ready and one is waiting. The New session pill sits at the bottom right.",
            css=CSS.format(k=k))

    # Your computer: one tmux window, six panes.
    wx, wy, ww, wh = 28, 70, 700, 590
    s.text(wx + 4, wy - 18, "ON YOUR COMPUTER", size=11, fill="faint", mono=True, extra=' letter-spacing="1.4"')
    s.rect(wx, wy, ww, wh, rx=14, fill="paper", stroke="border")
    for i in range(3):
        s.circle(wx + 22 + i * 16, wy + 20, 5, fill="tint", stroke="border")
    s.text(wx + ww / 2, wy + 25, "tmux · studio", size=12.5, fill="soft", anchor="middle", mono=True)
    s.line(wx, wy + 40, wx + ww, wy + 40, stroke="line")
    cols, rows = 2, 3
    pw, ph = (ww - 24) / cols, (wh - 52) / rows
    anchors = []
    for i, (client, title, _dir, _sub, st, _t, lines) in enumerate(SESSIONS):
        c, r = i % cols, i // cols
        px, py = wx + 12 + c * pw, wy + 46 + r * ph
        s.rect(px + 4, py + 4, pw - 8, ph - 8, rx=10, fill="card", stroke="line")
        client_badge(s, px + 26, py + 28, client, r=11)
        s.text(px + 44, py + 33, NAMES[client], size=12.5, mono=True, weight=500)
        s.text(px + pw - 18, py + 33, title, size=12.5, fill="soft", anchor="end")
        for j, ln in enumerate(lines):
            col = "sealText" if ln.startswith("?") else "ready" if ln.startswith("ok") else "ink"
            s.text(px + 20, py + 66 + j * 22, ln, size=13, fill=col, mono=True)
        anchors.append((wx + ww, py + ph / 2 + (-14 if c == 0 else 14)))

    # Your phone: Sessions.
    px, py, pw_, ph_ = 860, 24, 312, 656
    s.text(px - 20, wy - 18, "ON YOUR PHONE", size=11, fill="faint", mono=True, anchor="end", extra=' letter-spacing="1.4"')
    sx, sy, sw, sh = phone(s, px, py, pw_, ph_)
    bar = sy + 60
    round_button(s, sx + 30, bar, 17)
    menu_icon(s, sx + 30, bar)
    s.text(sx + sw / 2 - 42, bar + 5, "Brain", size=15, fill="soft", anchor="middle")
    s.text(sx + sw / 2 + 36, bar + 5, "Sessions", size=15, weight=600, anchor="middle")
    s.rect(sx + sw / 2 + 22, bar + 16, 28, 2.5, rx=1.25, fill="ink")
    dots_icon(s, sx + sw - 30, bar)

    y = bar + 52
    row_h = 60
    group = None
    ends = []
    for client, title, d, sub, st, t, _lines in SESSIONS:
        if d != group:
            group = d
            if y > bar + 60:
                y += 14
            s.text(sx + 20, y, d, size=12.5, fill="soft")
            y += 14
        client_badge(s, sx + 36, y + row_h / 2, client, r=16)
        s.text(sx + 62, y + 26, title, size=15, weight=500)
        s.text(sx + 62, y + 46, sub if len(sub) < 26 else sub[:24] + "…", size=12.5,
               fill="sealText" if st == "needs" else "soft", mono=client == "shell")
        s.text(sx + sw - 18, y + 24, t, size=11.5, fill="soft", anchor="end")
        if st == "needs":
            s.circle(sx + sw - 24, y + 42, 4, fill="seal")
        elif st:
            mark(s, st, sx + sw - 24, y + 42, r=6)
        ends.append((sx - 2, y + row_h / 2))
        y += row_h

    # New session: the page's one ink pill.
    nw = 132
    s.rect(sx + sw - 16 - nw, sy + sh - 72, nw, 40, rx=20, fill="ink")
    s.path(f"M{sx + sw - 16 - nw + 22} {sy + sh - 52}h12M{sx + sw - 16 - nw + 28} {sy + sh - 58}v12", stroke="paper", sw=1.6,
           extra=' stroke-linecap="round"')
    s.text(sx + sw - 16 - nw + 44, sy + sh - 47, "New session", size=14, fill="paper", weight=500)

    # Lanes: pane → row.
    for (ax, ay), (bx, by), sess in zip(anchors, ends, SESSIONS):
        mid = (ax + bx) / 2 + 20
        d = f"M{ax:.1f} {ay:.1f}C{mid:.1f} {ay:.1f},{mid - 40:.1f} {by:.1f},{bx:.1f} {by:.1f}"
        s.path(d, stroke="border", sw=1)
        col = C["seal"] if sess[4] == "needs" else C["faint"]
        s.path(d, stroke=col, sw=1.4, cls="flow")
        s.circle(bx, by, 2.5, fill=col)
        s.circle(ax, ay, 2.5, fill=col)
    return s.render()


if __name__ == "__main__":
    import sys
    sys.stdout.write(build())
