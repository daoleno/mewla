# Sessions: one phone, four states of the same Session.
# 1 New session (Shell is a preset), 2 Chat, 3 Terminal, 4 Git diff.
from common import C, Svg, phone, pill, dot

PRESETS = [("Shell", "$"), ("Claude", "C"), ("Codex", "X"), ("Cursor", "Cu"),
           ("Grok", "G"), ("Pi", "π"), ("OpenCode", "O"), ("DSH", "D")]

CSS = """
.{k} .cur{{animation:{k}-c 1.1s steps(1) infinite}}
@keyframes {k}-c{{50%{{opacity:0}}}}
@media (prefers-reduced-motion:reduce){{.{k} .cur{{animation:none}}}}
"""


def header(s, sx, sy, sw, title="onboarding", sub="claude · ~/zen"):
    y = sy + 52
    s.circle(sx + 30, y + 18, 18, fill="panel3")
    s.path(f"M{sx + 33} {y + 11}l-7 7 7 7", stroke="ink", sw=1.6)
    cw = 170
    s.rect(sx + sw / 2 - cw / 2, y, cw, 36, rx=18, fill="panel3")
    dot(s, sx + sw / 2 - cw / 2 + 18, y + 18, "run", r=3.5)
    s.text(sx + sw / 2 - cw / 2 + 30, y + 16, title, size=13, weight=600)
    s.text(sx + sw / 2 - cw / 2 + 30, y + 29, sub, size=10, fill="faint", mono=True)
    s.circle(sx + sw - 30, y + 18, 18, fill="panel3")
    for i in (-6, 0, 6):
        s.circle(sx + sw - 30 + i, y + 18, 1.6, fill="ink")


def tool_row(s, x, y, w, icon, title, detail, ok=True):
    s.rect(x, y, 22, 22, rx=6, fill="sageD")
    s.text(x + 11, y + 15, icon, size=11, fill="sage", anchor="middle", mono=True)
    s.text(x + 32, y + 11, title, size=11.5, fill="dim")
    s.text(x + 32, y + 24, detail, size=10.5, fill="faint", mono=True)


def build():
    k = "ss"
    W, H = 380, 760
    s = Svg(k, W, H,
            "One Session, four views on the phone",
            "A phone showing one claude Session in ~/zen. New session lists Shell, Claude, Codex, Cursor, Grok, Pi, OpenCode and DSH. "
            "Chat shows the request, tool rows and the reply. Terminal shows the same Session as the live terminal grid. "
            "Git shows three changed files and a diff.",
            states=4, css=CSS.format(k=k))
    sx, sy, sw, sh = phone(s, 10, 10, 360, 740)
    bot = sy + sh

    # ---- 1. New session sheet over a dimmed Sessions list
    s.g("st s1")
    for i in range(6):
        s.rect(sx + 14, sy + 120 + i * 50, sw - 28, 42, rx=12, fill="panel2")
        s.rect(sx + 30, sy + 134 + i * 50, 90 + (i * 37) % 70, 8, rx=4, fill="panel3")
    s.rect(sx, sy, sw, sh, rx=45, fill="#000", extra=' opacity=".55"')
    top = sy + 250
    s.path(f"M{sx} {top + 28}a28 28 0 0 1 28 -28H{sx + sw - 28}a28 28 0 0 1 28 28V{bot - 45}a45 45 0 0 1 -45 45H{sx + 45}a45 45 0 0 1 -45 -45Z", fill="panel2", stroke=None)
    s.rect(sx + sw / 2 - 18, top + 9, 36, 4, rx=2, fill="line2")
    s.text(sx + 22, top + 46, "New session", size=17, weight=600)
    s.rect(sx + 18, top + 62, sw - 36, 38, rx=12, fill="panel3")
    s.text(sx + 32, top + 86, "~/zen", size=12.5, mono=True)
    s.text(sx + sw - 32, top + 86, "Change", size=11.5, fill="sage", anchor="end")
    s.text(sx + 22, top + 126, "START WITH", size=9.5, fill="faint", mono=True, extra=' letter-spacing="1.2"')
    tw, th = (sw - 36 - 10) / 2, 44
    for i, (name, glyph) in enumerate(PRESETS):
        x = sx + 18 + (i % 2) * (tw + 10)
        y = top + 138 + (i // 2) * (th + 8)
        sel = name == "Claude"
        s.rect(x, y, tw, th, rx=12, fill="sageD" if sel else "panel3", stroke="sage2" if sel else None)
        s.rect(x + 10, y + 11, 22, 22, rx=6, fill="#0e0f0b")
        s.text(x + 21, y + 26, glyph, size=11, fill="sage" if sel else "dim", anchor="middle", mono=True, weight=500)
        s.text(x + 42, y + 27, name, size=13, fill="ink", weight=500)
    s.rect(sx + 18, bot - 74, sw - 36, 44, rx=22, fill="sage")
    s.text(sx + sw / 2, bot - 47, "Create", size=14, fill="#0c1a12", anchor="middle", weight=600)
    s.end()

    # ---- 2. Chat
    s.g("st s2")
    header(s, sx, sy, sw)
    y = sy + 116
    s.rect(sx + sw / 2 - 30, y, 60, 20, rx=10, fill="panel3")
    s.text(sx + sw / 2, y + 14, "Today", size=10, fill="faint", anchor="middle")
    y += 36
    bw = 230
    s.rect(sx + sw - 16 - bw, y, bw, 52, rx=18, fill="panel3")
    s.text(sx + sw - 16 - bw + 14, y + 22, "Make the empty state match", size=13)
    s.text(sx + sw - 16 - bw + 14, y + 39, "the new tokens.", size=13)
    y += 72
    tool_row(s, sx + 18, y, sw, "R", "Read", "components/ui/EmptyState.tsx")
    y += 36
    tool_row(s, sx + 18, y, sw, "E", "Edited 2 files", "+38 −12")
    y += 36
    tool_row(s, sx + 18, y, sw, "$", "Ran bunx tsc --noEmit", "exit 0 · 14.2s")
    y += 58
    for i, ln in enumerate(["EmptyState now reads the shared", "spacing and radius tokens, and the", "icon tile uses the accent ramp.",
                            "Typecheck passes."]):
        s.text(sx + 18, y + i * 20, ln, size=13.5, fill="ink")
    y += 96
    s.text(sx + 18, y, "PLAN", size=9.5, fill="faint", mono=True, extra=' letter-spacing="1.2"')
    for i, (txt, done) in enumerate([("Audit EmptyState usage", True), ("Swap tokens", True), ("Check dark mode", False)]):
        yy = y + 16 + i * 22
        if done:
            s.circle(sx + 25, yy, 6, fill="teal")
            s.path(f"M{sx + 22} {yy}l2 2 4 -4", stroke="#0c0d0a", sw=1.6)
        else:
            s.circle(sx + 25, yy, 5.5, stroke="faint", sw=1.2)
        s.text(sx + 38, yy + 4, txt, size=12, fill="dim" if done else "ink")
    # composer
    cy = bot - 70
    s.rect(sx + 14, cy, sw - 28, 46, rx=23, fill="panel3", stroke="line2")
    s.circle(sx + 37, cy + 23, 15, fill="sageD")
    s.path(f"M{sx + 37} {cy + 17}v12M{sx + 31} {cy + 23}h12", stroke="sage", sw=1.6)
    s.text(sx + 60, cy + 27, "Message claude", size=13, fill="faint")
    s.circle(sx + sw - 37, cy + 23, 15, fill="sage")
    s.path(f"M{sx + sw - 37} {cy + 29}v-12m-5 5l5 -5 5 5", stroke="#0c1a12", sw=1.8)
    s.end()

    # ---- 3. Terminal (same Session, live grid)
    s.g("st s3")
    header(s, sx, sy, sw)
    ty = sy + 106
    s.rect(sx, ty, sw, bot - ty - 66, fill="#090a07")
    rows = [
        ("> ", "sage", "Make the empty state match the new tokens."),
        ("", "", ""),
        ("● ", "teal", "Read(components/ui/EmptyState.tsx)"),
        ("  ⎿ ", "faint", "Read 142 lines"),
        ("● ", "teal", "Update(EmptyState.tsx, tokens.ts)"),
        ("  ⎿ ", "faint", "+38 −12"),
        ("● ", "teal", "Bash(bunx tsc --noEmit)"),
        ("  ⎿ ", "faint", "exit 0 · 14.2s"),
        ("", "", ""),
        ("● ", "ink", "EmptyState now reads the shared"),
        ("  ", "", "spacing and radius tokens, and"),
        ("  ", "", "the icon tile uses the accent"),
        ("  ", "", "ramp. Typecheck passes."),
    ]
    for i, (pre, col, txt) in enumerate(rows):
        yy = ty + 26 + i * 19
        if pre:
            s.text(sx + 14, yy, pre, size=11.5, fill=col or "dim", mono=True)
        s.text(sx + 14 + len(pre) * 7.1, yy, txt, size=11.5, fill="ink" if col in ("sage", "ink") else "dim", mono=True)
    by = ty + 26 + len(rows) * 19 + 10
    s.rect(sx + 12, by, sw - 24, 34, rx=4, stroke="line2")
    s.text(sx + 24, by + 22, ">", size=12, fill="sage", mono=True)
    s.rect(sx + 38, by + 10, 8, 15, fill="ink", cls="cur")
    s.text(sx + 14, by + 54, "? for shortcuts", size=10.5, fill="faint", mono=True)
    s.text(sx + sw - 14, by + 54, "opus 5.5 · high", size=10.5, fill="faint", mono=True, anchor="end")
    # key accessory row
    ky = bot - 60
    x = sx + 12
    for key in ["esc", "tab", "ctrl", "↑", "↓", "←", "→", "/"]:
        w = 34 if len(key) > 1 else 30
        s.rect(x, ky, w, 30, rx=8, fill="panel3")
        s.text(x + w / 2, ky + 19, key, size=11, fill="dim", anchor="middle", mono=True)
        x += w + 6
    s.end()

    # ---- 4. Git diff sheet
    s.g("st s4")
    header(s, sx, sy, sw)
    gy = sy + 112
    s.rect(sx + 8, gy, sw - 16, bot - gy - 8, rx=26, fill="panel2")
    s.text(sx + 26, gy + 34, "Changes", size=17, weight=600)
    s.text(sx + 26, gy + 53, "3 files · ", size=11.5, fill="dim")
    s.text(sx + 80, gy + 53, "+41", size=11.5, fill="teal", mono=True)
    s.text(sx + 108, gy + 53, "−12", size=11.5, fill="red", mono=True)
    # comparison segmented
    seg = ["All", "Working", "Staged"]
    segw = (sw - 52) / 3
    s.rect(sx + 26, gy + 68, sw - 52, 30, rx=15, fill="panel3")
    s.rect(sx + 28, gy + 70, segw - 4, 26, rx=13, fill="#3a3b33")
    for i, t in enumerate(seg):
        s.text(sx + 26 + segw * i + segw / 2, gy + 88, t, size=11.5, fill="ink" if i == 0 else "dim", anchor="middle", weight=500 if i == 0 else 400)
    files = [("M", "components/ui/EmptyState.tsx", "+29 −9"), ("M", "constants/tokens.ts", "+6 −3"), ("A", "ui/emptyState.test.ts", "+6")]
    for i, (st, f, n) in enumerate(files):
        y = gy + 112 + i * 40
        s.rect(sx + 22, y, sw - 44, 34, rx=10, fill="panel3" if i == 0 else "none")
        s.rect(sx + 30, y + 7, 20, 20, rx=5, fill="sageD" if st == "A" else "#2b2a1f")
        s.text(sx + 40, y + 21, st, size=11, fill="teal" if st == "A" else "amber", anchor="middle", mono=True, weight=500)
        s.text(sx + 60, y + 21, f, size=11.5, fill="ink", mono=True)
        s.text(sx + sw - 30, y + 21, n, size=10.5, fill="faint", mono=True, anchor="end")
    dy = gy + 250
    s.text(sx + 26, dy, "EmptyState.tsx", size=11, fill="faint", mono=True)
    lines = [
        (" ", "41", "export function EmptyState({"),
        ("-", "42", "  padding: 24,"),
        ("-", "43", "  borderRadius: 18,"),
        ("+", "42", "  padding: Spacing.xl,"),
        ("+", "43", "  borderRadius: Radii.card,"),
        (" ", "44", "  gap: Spacing.md,"),
        ("-", "45", "  backgroundColor: '#1c1c18',"),
        ("+", "45", "  backgroundColor: colors.surface,"),
        (" ", "46", "})"),
    ]
    for i, (m, n, code) in enumerate(lines):
        y = dy + 14 + i * 22
        fill = {"+": "#16302a", "-": "#3a1f1c", " ": "none"}[m]
        s.rect(sx + 18, y, sw - 36, 22, fill=fill)
        s.text(sx + 30, y + 15, n, size=10.5, fill="faint", mono=True)
        s.text(sx + 54, y + 15, m, size=11, fill={"+": "teal", "-": "red", " ": "faint"}[m], mono=True)
        s.text(sx + 66, y + 15, code, size=11, fill="ink" if m != " " else "dim", mono=True)
    s.end()

    return s.render(bg=False)


if __name__ == "__main__":
    import sys
    sys.stdout.write(build())
