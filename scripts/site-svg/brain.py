# Brain: a goal becomes Work, routing.md picks the Worker, Brain reviews.
# States: 1 Plan, 2 Delegate, 3 Review.
from common import C, Svg, dot, pill

ROUTES = [
    ("Tiny mechanical edit", "pi", "fast model", "low"),
    ("Routine implementation", "codex", "default", "medium"),
    ("Visual design, frontend, copy", "claude", "default", "high"),
    ("Hard design or debugging", "claude", "strongest", "high"),
]
WORK = [
    ("w_41", "Android crash on resume", 3),
    ("w_42", "Session list filters", 1),
    ("w_43", "Changelog for 0.2", 0),
]
REVIEW = [("accepted", "teal"), ("sent back · add a test", "amber"), ("accepted", "teal")]

CSS = """
.{k} .flow{{stroke-dasharray:4 5;animation:{k}-f 1.6s linear infinite}}
@keyframes {k}-f{{to{{stroke-dashoffset:-18}}}}
@media (prefers-reduced-motion:reduce){{.{k} .flow{{animation:none}}}}
"""


def brain_mark(s, cx, cy, r=26):
    s.circle(cx, cy, r + 8, stroke="sageD", sw=1)
    s.circle(cx, cy, r, fill="#10140f", stroke="sage2", sw=1.2)
    sc = r / 512 * 1.15
    s.add(f'<g transform="translate({cx - 512 * sc:g} {cy - 512 * sc:g}) scale({sc:g})" fill="none" stroke="{C["ink"]}" stroke-linecap="round" stroke-linejoin="round">'
          '<path d="M790 372C724 252 590 209 461 244C334 279 252 393 266 524C282 675 411 778 558 760C650 749 731 697 781 620" stroke-width="64"/>'
          '<path d="M334 430C470 352 622 334 665 371C694 396 671 440 601 495C486 586 401 663 437 705C472 747 606 730 722 801" stroke-width="78"/></g>')


def build(narrow=False):
    k = "bn" if narrow else "bw"
    W, H = (400, 760) if narrow else (1100, 376)
    s = Svg(k, W, H,
            "Brain turns a goal into Work and routes each part",
            "You send Brain one goal. Brain writes three Work items, reads routing.md and gives each to a Worker: "
            "the crash to claude at high reasoning, the filters to codex at medium, the changelog to pi at low. "
            "Brain reviews the results, accepts two and sends one back for a test.",
            states=3, css=CSS.format(k=k))

    if narrow:
        gx, gy, gw = 16, 16, 368
        bx, by = 44, 160
        rx_, ry_, rw = 16, 218, 368
        cx_, cy_, cw = 16, 452, 368
    else:
        gx, gy, gw = 24, 40, 270
        bx, by = 159, 240
        rx_, ry_, rw = 340, 40, 340
        cx_, cy_, cw = 730, 40, 346

    # Goal, as sent from the phone.
    s.text(gx, gy + 4, "YOU, FROM THE PHONE", size=9.5, fill="faint", mono=True, extra=' letter-spacing="1.2"')
    s.rect(gx, gy + 16, gw, 86 if not narrow else 70, rx=18, fill="panel3")
    goal = (["Ship the 0.2 beta: fix the resume", "crash, add session filters and", "write the changelog."]
            if not narrow else ["Ship the 0.2 beta: fix the resume crash,", "add session filters and write the changelog."])
    for i, ln in enumerate(goal):
        s.text(gx + 16, gy + 42 + i * 19, ln, size=13.5 if not narrow else 12.5)

    brain_mark(s, bx, by)
    s.text(bx + (0 if not narrow else 48), by + (58 if not narrow else -3), "Brain", size=14, weight=600, anchor="middle" if not narrow else "start")
    s.text(bx + (0 if not narrow else 48), by + (76 if not narrow else 14), "on your computer", size=10.5, fill="faint", mono=True, anchor="middle" if not narrow else "start")
    if not narrow:
        s.line(bx, gy + 108, bx, by - 38, stroke="line2")

    # routing.md
    rh = 44 + len(ROUTES) * 40
    s.rect(rx_, ry_, rw, rh, rx=14, fill="panel", stroke="line")
    s.text(rx_ + 16, ry_ + 26, "routing.md", size=12, mono=True, weight=500)
    s.text(rx_ + rw - 16, ry_ + 26, "~/.mewla/brain/workspace", size=10, fill="faint", mono=True, anchor="end")
    s.line(rx_, ry_ + 40, rx_ + rw, ry_ + 40, stroke="line")
    used = {w[2]: i for i, w in enumerate(WORK)}
    for i, (task, ex, model, rsn) in enumerate(ROUTES):
        y = ry_ + 44 + i * 40
        if i in used:
            s.rect(rx_ + 6, y + 2, rw - 12, 36, rx=8, fill="sageD", cls="st s2 s3")
        s.text(rx_ + 16, y + 17, task, size=12, fill="ink")
        s.text(rx_ + 16, y + 32, f"{ex} · {model} · {rsn}", size=10.5, fill="faint", mono=True)
        if i in used:
            s.text(rx_ + 16, y + 32, f"{ex} · {model} · {rsn}", size=10.5, fill="sage", mono=True, cls="st s2 s3")

    # Work cards
    for j, (wid, title, route) in enumerate(WORK):
        y = cy_ + j * (100 if not narrow else 98)
        ex, model, rsn = ROUTES[route][1], ROUTES[route][2], ROUTES[route][3]
        s.rect(cx_, y, cw, 86, rx=14, fill="panel2", stroke="line")
        s.text(cx_ + 16, y + 24, wid, size=10.5, fill="faint", mono=True)
        s.text(cx_ + 58, y + 24, title, size=13.5, weight=500)
        # state 1: planned
        s.g("st s1")
        s.text(cx_ + 16, y + 50, "Work recorded · objective and finish line", size=11, fill="dim")
        pill(s, cx_ + 16, y + 60, "planned", mono=True, size=10)
        s.end()
        # state 2: delegated, running
        s.g("st s2")
        s.text(cx_ + 16, y + 50, f"Worker · {ex} · {rsn} reasoning", size=11, fill="dim", mono=True)
        dot(s, cx_ + 22, y + 69, "run", r=3.5)
        s.text(cx_ + 32, y + 73, "running in tmux · open as Chat or Terminal", size=10.5, fill="sage")
        s.end()
        # state 3: reviewed
        s.g("st s3")
        label, col = REVIEW[j]
        s.text(cx_ + 16, y + 50, ["diff + repro checked", "no test for the filter state", "matches the merged commits"][j], size=11, fill="dim")
        s.rect(cx_ + 16, y + 60, 9, 9, rx=2, fill=col)
        s.text(cx_ + 32, y + 69, label, size=11, fill=col, mono=True)
        s.end()

        # connectors: route row -> card (wide only)
        if not narrow:
            ry = ry_ + 44 + route * 40 + 20
            cy = y + 43
            s.path(f"M{rx_ + rw} {ry}C{rx_ + rw + 30} {ry},{cx_ - 30} {cy},{cx_} {cy}", stroke="sage2", sw=1.2, cls="st s2 flow")
            s.path(f"M{rx_ + rw} {ry}C{rx_ + rw + 30} {ry},{cx_ - 30} {cy},{cx_} {cy}", stroke="line2", sw=1, cls="st s3")
    if not narrow:
        # Brain -> routing
        s.path(f"M{bx + 36} {by}C{bx + 90} {by},{rx_ - 50} {ry_ + rh / 2},{rx_} {ry_ + rh / 2}", stroke="line2", sw=1)
        s.text(bx + 104, by + 76, "reads before every spawn", size=10.5, fill="faint", mono=True)
    else:
        s.line(bx, by + 36, bx, ry_ - 6, stroke="line2")
        s.text(bx + 14, ry_ - 12, "reads before every spawn", size=10, fill="faint", mono=True)

    foot_y = H - 18
    s.text(24 if not narrow else 16, foot_y, "Only Brain or you can close Work. An exited process never counts as done.", size=11 if not narrow else 9.6, fill="faint", cls="st s3")
    return s.render()


if __name__ == "__main__":
    import sys
    sys.stdout.write(build("--narrow" in sys.argv))
