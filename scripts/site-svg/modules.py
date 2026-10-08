# One drawing per tool screen, drawn from the app's Seal & Slip screens with
# the atlas-notes demo data. Each is a phone-width screen on paper.
from common import (C, Svg, chevron, client_badge, dots_icon, logo, mark, round_button, screen_header,
                    seal, segmented, state, tw)

W = 480


def screen(k, h, title, desc):
    s = Svg(k, W, h, title, desc)
    s.rect(12, 12, W - 24, h - 24, rx=22, fill="paper", stroke="border")
    return s, 12, 12, W - 24


def card(s, x, y, w, h):
    s.rect(x, y, w, h, rx=16, fill="card", stroke="line")


def caption(s, x, y, text):
    s.text(x, y, text, size=13, fill="soft")


def server_line(s, x, y, w, word="Connected", kind="ready"):
    s.rect(x, y - 11, 13, 10, rx=2, stroke="soft", sw=1.3)
    s.line(x + 3, y + 2, x + 10, y + 2, stroke="soft", sw=1.3)
    s.text(x + 22, y, "Studio Mac", size=13.5)
    state(s, kind, x + w, y, size=12.5, word=word)


def models():
    s, x, y, w = screen("pm", 540, "Model Providers for Codex",
                        "Codex can use the official login or your own OpenAI, DeepSeek or OpenRouter keys. DeepSeek is selected. "
                        "The Mewla Provider Gateway is ready.")
    screen_header(s, x, y + 6, w, "Model Providers")
    sx, sw = x + 18, w - 36
    s.rect(sx, y + 66, sw, 44, rx=22, fill="tint")
    s.rect(sx + 4, y + 70, sw / 2 - 8, 36, rx=18, fill="card", stroke="line")
    logo(s, "codex", sx + sw / 4 - 38, y + 79, 18)
    s.text(sx + sw / 4 - 14, y + 93, "Codex", size=14, weight=500)
    logo(s, "claude", sx + 3 * sw / 4 - 54, y + 79, 18)
    s.text(sx + 3 * sw / 4 - 30, y + 93, "Claude Code", size=14, fill="soft")
    caption(s, sx + 4, y + 140, "Codex uses")
    s.text(sx + sw - 4, y + 140, "+ Add", size=13.5, weight=500, anchor="end")
    rows = [("Official login", "Direct · uses the native account", False, False),
            ("OpenAI", "api.openai.com · 14 models", False, True),
            ("DeepSeek", "api.deepseek.com · 3 models", True, True),
            ("OpenRouter", "openrouter.ai · 212 models", False, True)]
    cy = y + 154
    card(s, sx, cy, sw, 64 * len(rows))
    for i, (name, sub, on, more) in enumerate(rows):
        ry = cy + i * 64
        if i:
            s.line(sx + 52, ry, sx + sw, ry, stroke="line")
        s.circle(sx + 26, ry + 32, 9, stroke="ink" if on else "faint", sw=1.6)
        if on:
            s.circle(sx + 26, ry + 32, 4.5, fill="ink")
        s.text(sx + 52, ry + 28, name, size=15, weight=500)
        s.text(sx + 52, ry + 47, sub, size=12.5, fill="soft")
        if more:
            dots_icon(s, sx + sw - 24, ry + 32, col="soft")
    gy = cy + 64 * len(rows) + 34
    caption(s, sx + 4, gy, "Gateway")
    card(s, sx, gy + 12, sw, 52)
    s.text(sx + 18, gy + 43, "Mewla Provider Gateway", size=15, weight=500)
    state(s, "ready", sx + sw - 16, gy + 43, size=12.5)
    return s.render()


def skills():
    s, x, y, w = screen("sk", 480, "Skills and Agent Plugins",
                        "The Skills your agents load on Studio Mac, with the agent that loads each one, a search field "
                        "and a delete action per row.")
    screen_header(s, x, y + 6, w, "Skills")
    sx, sw = x + 18, w - 36
    server_line(s, sx, y + 86, sw)
    segmented(s, sx, y + 104, sw, ["Skills", "Agent Plugins"])
    s.rect(sx, y + 158, sw, 44, rx=12, fill="card", stroke="line")
    s.circle(sx + 22, y + 178, 6.5, stroke="soft", sw=1.5)
    s.line(sx + 27, y + 183, sx + 31, y + 187, stroke="soft", sw=1.5)
    s.text(sx + 42, y + 185, "Search Skills", size=14.5, fill="faint")
    rows = [("codex", "release-notes", "Write release notes from merged pull requests"),
            ("codex", "go-tests", "Run and triage the Go test suite"),
            ("claude", "brand-voice", "Check copy against the brand voice guide"),
            ("claude", "design-audit", "Check a screen against the design language")]
    ry = y + 218
    for client, name, desc in rows:
        logo(s, client, sx + 2, ry + 14, 16)
        s.text(sx + 30, ry + 26, name, size=15, weight=500)
        s.text(sx + 30, ry + 47, desc if len(desc) < 42 else desc[:40] + "…", size=13, fill="soft")
        # trash
        tx, ty = sx + sw - 18, ry + 30
        s.path(f"M{tx - 6} {ty - 6}h12M{tx - 2} {ty - 8.5}h4M{tx - 4.5} {ty - 6}l.8 12h7.4l.8-12", stroke="soft", sw=1.3,
               extra=' stroke-linecap="round" stroke-linejoin="round"')
        s.line(sx, ry + 62, sx + sw, ry + 62, stroke="line")
        ry += 62
    return s.render()


def plugins():
    s, x, y, w = screen("pl", 500, "Plugins: one row per service",
                        "GitHub and Linear are connected, Notion needs a reconnect with a Reconnect button on its row, "
                        "and Google has a Connect button. MCP and OpenAPI services are added under Your own services.")
    screen_header(s, x, y + 6, w, "Plugins")
    sx, sw = x + 18, w - 36

    def row_button(right, cy, word):
        bw = tw(word, 13, weight=600) + 28
        s.rect(right - bw, cy - 16, bw, 32, rx=16, fill="pressed")
        s.text(right - bw / 2, cy + 5, word, size=13, weight=600, anchor="middle")

    def rows(top, items):
        card(s, sx, top, sw, 60 * len(items))
        for i, (lg, name, sub, kind, word) in enumerate(items):
            ry = top + i * 60
            if i:
                s.line(sx + 56, ry, sx + sw, ry, stroke="line")
            logo(s, lg, sx + 18, ry + 20, 20)
            s.text(sx + 56, ry + 27, name, size=15, weight=500)
            s.text(sx + 56, ry + 46, sub, size=12.5, fill="soft")
            if kind == "button":
                row_button(sx + sw - 34, ry + 30, word)
            else:
                state(s, kind, sx + sw - 34, ry + 35, size=12.5, word=word)
            chevron(s, sx + sw - 22, ry + 30, 5)

    caption(s, sx + 4, y + 96, "Services")
    services = [("github", "GitHub", "atlas-notes", "ready", "Connected"),
                ("notion", "Notion", "Atlas workspace", "button", "Reconnect"),
                ("linear", "Linear", "Atlas team", "ready", "Connected"),
                ("google", "Google", "Gmail, Drive and Calendar", "button", "Connect")]
    rows(y + 108, services)
    ay = y + 108 + 60 * len(services) + 34
    caption(s, sx + 4, ay, "Your own services")
    rows(ay + 12, [("mcp", "Remote MCP", "Any MCP server, by its address", "button", "Add")])
    return s.render()


def stats():
    s, x, y, w = screen("st", 560, "Usage per model",
                        "Stats for all activity: a Codex subscription limit, total cost and tokens, then cost per model. "
                        "Reported costs are exact, estimated ones carry a ≈ and unknown ones a dash.")
    sx, sw = x + 18, w - 36
    for i, hgt in enumerate((7, 13, 10)):
        s.rect(sx + 2 + i * 5, y + 40 - hgt, 3, hgt, rx=1, fill="ink")
    s.text(sx + 26, y + 34, "Stats", size=17, weight=600)
    s.text(sx + 26, y + 52, "All activity", size=12, fill="soft")
    card(s, sx, y + 70, sw, 136)
    s.text(sx + 18, y + 100, "Codex subscription", size=16, weight=600)
    s.text(sx + 18, y + 119, "ChatGPT Plus", size=12.5, fill="soft")
    for i, (lab, left, v) in enumerate((("5 hours", "66% left", .34), ("1 week", "82% left", .18))):
        by = y + 146 + i * 30
        s.text(sx + 18, by, lab, size=13)
        s.text(sx + sw - 18, by, left, size=12.5, mono=True, anchor="end")
        s.rect(sx + 18, by + 8, sw - 36, 5, rx=2.5, fill="pressed")
        s.rect(sx + 18, by + 8, (sw - 36) * v, 5, rx=2.5, fill="ink")
    card(s, sx, y + 218, sw, 84)
    s.text(sx + 18, y + 244, "Cost · this month", size=12.5, fill="soft")
    s.text(sx + 18, y + 282, "≈ $182.20", size=30, weight=800, display=True)
    s.line(sx + sw / 2 + 20, y + 232, sx + sw / 2 + 20, y + 290, stroke="line")
    s.text(sx + sw / 2 + 38, y + 244, "Tokens", size=12.5, fill="soft")
    s.text(sx + sw / 2 + 38, y + 282, "48.1M", size=30, weight=800, display=True)
    s.text(sx, y + 340, "Models", size=17, weight=600)
    rows = [("claude-opus-5-5", "$96.40", "18.3M tokens · 41 sessions", .53),
            ("gpt-5", "≈ $61.10", "14.9M tokens · 52 sessions", .34),
            ("deepseek-v4-flash", "$24.70", "6.2M tokens · 27 sessions", .14),
            ("gpt-6-astra", "—", "8.7M tokens · 6 sessions", 0)]
    ry = y + 372
    for name, cost, sub, v in rows:
        s.text(sx, ry, name, size=13.5, mono=True)
        s.text(sx + sw, ry, cost, size=13.5, mono=True, anchor="end")
        s.text(sx, ry + 18, sub, size=12, fill="soft")
        s.rect(sx + sw - 110, ry + 11, 110, 4, rx=2, fill="pressed")
        if v:
            s.rect(sx + sw - 110, ry + 11, 110 * v / .53, 4, rx=2, fill="ink")
        ry += 40
    return s.render()


def calendar():
    s, x, y, w = screen("ca", 530, "Calendar with a scheduled action",
                        "Today: a reminder and a scheduled action that is running as Work. Tomorrow: an event. "
                        "Friday: a deadline that needs you. Monday: a scheduled action, Check 1.4 crash reports.")
    sx, sw = x + 18, w - 36
    s.text(x + w / 2, y + 38, "Calendar", size=18, weight=600, anchor="middle")
    s.path(f"M{x + w - 30} {y + 26}v14M{x + w - 37} {y + 33}h14", stroke="ink", sw=1.6, extra=' stroke-linecap="round"')

    def day(label, sub, yy):
        s.text(sx, yy, label, size=20, weight=800, display=True)
        if sub:
            s.text(sx, yy + 22, sub, size=12.5, fill="soft")

    def item(yy, time, title, kind_word, mk=None, word=None):
        s.text(sx + 4, yy + 30, time, size=13, fill="soft")
        s.line(sx + 62, yy + 6, sx + 62, yy + 58, stroke="border", sw=1.5)
        s.text(sx + 78, yy + 24, title, size=15, weight=500)
        s.text(sx + 78, yy + 46, kind_word, size=12.5, fill="soft")
        if mk:
            state(s, mk, sx + 78 + tw(kind_word, 12.5) + 8, yy + 46, anchor="start", size=12.5, word=word)
        chevron(s, sx + sw - 8, yy + 30, 5)

    day("Today", "Thursday, October 8", y + 86)
    item(y + 120, "10:30", "Review the 1.4 launch checklist", "Reminder · Scheduled")
    item(y + 184, "14:00", "Publish the 1.4 release notes", "Mewla action ·", "running", "Running")
    day("Friday", None, y + 286)
    item(y + 300, "17:00", "atlas-notes 1.4 ships", "Deadline ·", "needs", "Due")
    day("Monday", None, y + 402)
    item(y + 416, "09:00", "Check 1.4 crash reports", "Mewla action · Scheduled")
    return s.render()


def services():
    s, x, y, w = screen("sv", 440, "Services with a Quick Tunnel",
                        "Ports your Sessions opened, grouped by project: vite on 5173 is shared through a temporary "
                        "Cloudflare Quick Tunnel, an API on 8080, and a persistent postgres bound to 127.0.0.1.")
    sx, sw = x + 18, w - 36
    s.text(sx, y + 40, "Services", size=20, weight=800, display=True)
    s.text(sx, y + 60, "3 ports", size=12.5, fill="soft")
    round_button(s, x + w - 30, y + 38, 16)
    s.path(f"M{x + w - 36} {y + 32}l12 12M{x + w - 24} {y + 32}l-12 12", stroke="ink", sw=1.5, extra=' stroke-linecap="round"')

    def group(yy, label, rows):
        caption(s, sx + 4, yy, label)
        card(s, sx, yy + 12, sw, 62 * len(rows))
        for i, (port, name, sub, tag) in enumerate(rows):
            ry = yy + 12 + i * 62
            if i:
                s.line(sx + 84, ry, sx + sw, ry, stroke="line")
            s.text(sx + 18, ry + 37, port, size=17, weight=600, mono=True)
            s.text(sx + 84, ry + 28, name, size=15, weight=500)
            s.text(sx + 84, ry + 47, sub, size=12.5, fill="soft")
            if tag:
                tw_ = tw(tag, 12, weight=500) + 22
                s.rect(sx + sw - 16 - tw_, ry + 20, tw_, 22, rx=11, stroke="ink")
                s.text(sx + sw - 16 - tw_ / 2, ry + 35, tag, size=12, weight=500, anchor="middle")
        return yy + 12 + 62 * len(rows)

    gy = group(y + 98, "Sessions · atlas-notes", [(":5173", "vite", "Sync fix · codex", "Public"),
                                                  (":8080", "go run ./cmd/api", "Settings copy · claude", None)])
    gy = group(gy + 32, "Persistent · weather-kit", [(":5432", "postgres", "127.0.0.1 · pg.service", None)])
    logo(s, "cloudflare", sx + 4, gy + 22, 18)
    s.text(sx + 30, gy + 36, "https://atlas-notes-sync.trycloudflare.com", size=12, fill="soft", mono=True)
    return s.render()


def spark(s, x, y, w, h, pts, fill=True):
    n = len(pts)
    xy = [(x + w * i / (n - 1), y + h - h * v) for i, v in enumerate(pts)]
    d = "M" + "L".join(f"{a:.1f} {b:.1f}" for a, b in xy)
    if fill:
        s.path(d + f"L{x + w} {y + h}L{x} {y + h}Z", stroke=None, fill="tint")
    s.path(d, stroke="ink", sw=1.4, extra=' stroke-linejoin="round"')


def resources():
    s, x, y, w = screen("rs", 660, "Resources dashboard",
                        "Studio Mac at normal pressure: CPU 41%, memory 78%, disk I/O 4.8 MB/s and pressure 0.4%, "
                        "then the processes using them, attributed to Workers, Brain, Docker or you.")
    screen_header(s, x, y + 6, w, "Resources")
    sx, sw = x + 18, w - 36
    s.text(sx, y + 92, "Studio Mac", size=19, weight=600)
    state(s, "ready", sx + 118, y + 92, anchor="start", size=12.5, word="Normal")
    s.text(sx, y + 114, "Live · 5 s poll", size=12.5, fill="soft")
    cards = [("CPU", "41%", "12 cores · peak 61%", [.3, .5, .35, .6, .4, .55, .45, .41]),
             ("Memory", "78%", "24.8 GB / 32 GB", [.7, .72, .74, .75, .76, .77, .78, .78]),
             ("Disk I/O", "4.8 MB/s", "R 2.8 · W 2.0 MB/s", [.2, .7, .3, .1, .6, .4, .8, .3]),
             ("Pressure", "0.4%", "Memory · some 10 s", [.05, .04, .06, .05, .04, .05, .05, .04])]
    cw = (sw - 12) / 2
    for i, (lab, val, sub, pts) in enumerate(cards):
        cx, cy = sx + (i % 2) * (cw + 12), y + 132 + (i // 2) * 150
        card(s, cx, cy, cw, 138)
        s.text(cx + 14, cy + 26, lab, size=13.5)
        s.text(cx + 14, cy + 56, val, size=22, weight=800, display=True)
        spark(s, cx + 14, cy + 68, cw - 28, 34, pts)
        s.text(cx + 14, cy + 124, sub, size=12, fill="soft")
    ty = y + 450
    s.text(sx, ty, "Consumers", size=17, weight=600)
    chips = ["All · 4", "Workers · 2", "Brain · 1", "Docker · 1"]
    cx = sx
    for i, c in enumerate(chips):
        cwid = tw(c, 12.5) + 24
        s.rect(cx, ty + 14, cwid, 28, rx=8, fill="card" if i else "select", stroke="ink" if not i else "border")
        s.text(cx + cwid / 2, ty + 33, c, size=12.5, anchor="middle")
        cx += cwid + 8
    rows = [("Sync fix", "Worker · codex", "1.2 GB"), ("Brain", "Brain · claude", "420 MB"), ("postgres 16", "Docker", "310 MB")]
    ry = ty + 60
    for name, sub, rss in rows:
        s.text(sx, ry + 16, name, size=14.5, weight=500)
        s.text(sx, ry + 34, sub, size=12, fill="soft")
        s.text(sx + sw, ry + 24, rss, size=13.5, mono=True, anchor="end")
        ry += 44
    return s.render()


def alerts():
    s, x, y, w = screen("al", 540, "Push alerts and Telegram",
                        "Push notifications only when a Session needs input, fails or finishes. Below, Telegram as a "
                        "second channel to the same Brain thread.")
    sx, sw = x + 18, w - 36
    s.text(sx, y + 34, "PUSH", size=11, fill="faint", mono=True, extra=' letter-spacing="1.4"')
    notes = [("Sync fix needs input", "Keep both copies, or the newest edit?"),
             ("Release notes failed", "Notion said 401: the token expired."),
             ("Conflict wording finished", "Five apps compared. “Conflicted copy” wins.")]
    ny = y + 46
    for t, b in notes:
        s.rect(sx, ny, sw, 64, rx=18, fill="card", stroke="line")
        seal(s, sx + 14, ny + 14, 34)
        s.text(sx + 60, ny + 28, t, size=14.5, weight=600)
        s.text(sx + 60, ny + 48, b, size=13, fill="soft")
        s.text(sx + sw - 14, ny + 28, "now" if t.startswith("Sync") else "2m", size=11.5, fill="soft", anchor="end")
        ny += 74
    s.text(sx, ny + 20, "Nothing for routine progress.", size=13, fill="soft")
    ty = ny + 58
    s.text(sx, ty, "TELEGRAM", size=11, fill="faint", mono=True, extra=' letter-spacing="1.4"')
    card(s, sx, ty + 12, sw, 164)
    logo(s, "telegram", sx + 16, ty + 26, 20)
    s.text(sx + 44, ty + 41, "Mewla Brain", size=14, weight=600)
    s.line(sx, ty + 56, sx + sw, ty + 56, stroke="line")
    s.rect(sx + sw - 148, ty + 68, 132, 32, rx=16, fill="me")
    s.text(sx + sw - 82, ty + 89, "where are we?", size=13.5, anchor="middle")
    s.rect(sx + 16, ty + 108, sw - 80, 54, rx=14, fill="tint")
    s.text(sx + 30, ty + 130, "2 of 5 back. Sync fix needs you:", size=13)
    s.text(sx + 30, ty + 150, "keep both copies, or the newest edit?", size=13)
    return s.render()


ALL = {"models": models, "skills": skills, "plugins": plugins, "resources": resources, "stats": stats,
       "calendar": calendar, "services": services, "alerts": alerts}

if __name__ == "__main__":
    import sys
    sys.stdout.write(ALL[sys.argv[1]]())
