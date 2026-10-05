# The rest of the product, one small instrument each. 480 wide so they stay
# legible at phone width; the page drives data-state from real buttons.
from common import C, Svg, dot, pill

W = 480


def seg(s, x, y, w, labels, state_of):
    """Segmented control drawn in the figure, following data-state."""
    n = len(labels)
    sw = w / n
    s.rect(x, y, w, 30, rx=15, fill="panel3")
    for i, lab in enumerate(labels):
        cls = "st " + " ".join(f"s{v}" for v in state_of[i])
        s.rect(x + 2 + sw * i, y + 2, sw - 4, 26, rx=13, fill="#3a3b33", cls=cls)
        s.text(x + sw * i + sw / 2, y + 19.5, lab, size=12, fill="ink", anchor="middle", weight=500)


def radio(s, x, y, on, cls=""):
    s.circle(x, y, 7.5, stroke="sage" if on else "faint", sw=1.4, cls=cls)
    if on:
        s.circle(x, y, 3.8, fill="sage", cls=cls)


def models():
    s = Svg("mp", W, 300, "Model Providers",
            "Model Providers for Codex and Claude. Codex uses the official login; OpenAI, DeepSeek and OpenRouter are saved providers, "
            "DeepSeek still needs a key. Claude uses the Anthropic API. The Zen Provider Gateway is running.",
            states=2)
    seg(s, 20, 20, 200, ["Codex", "Claude"], [[1], [2]])
    s.text(W - 20, 40, "Add", size=12, fill="sage", anchor="end", weight=500)
    lists = {
        1: [("Official login", "signed in on this host", True, None),
            ("OpenAI", "api.openai.com · 14 models · 2d", False, None),
            ("DeepSeek", "api.deepseek.com · 3 models", False, "Key required"),
            ("OpenRouter", "openrouter.ai · 212 models · 5h", False, None)],
        2: [("Official login", "not signed in", False, None),
            ("Anthropic", "api.anthropic.com · 9 models · 1d", True, None),
            ("Custom Gateway", "llm.home.lan · 6 models", False, None),
            ("DeepSeek", "api.deepseek.com · 3 models", False, "Key required")],
    }
    for st, rows in lists.items():
        s.g(f"st s{st}")
        s.rect(20, 66, W - 40, 4 * 46, rx=14, fill="panel2")
        for i, (name, sub, on, tag) in enumerate(rows):
            y = 66 + i * 46
            if i:
                s.line(52, y, W - 20, y, stroke="line")
            radio(s, 40, y + 23, on)
            s.text(58, y + 20, name, size=13, weight=500)
            s.text(58, y + 35, sub, size=10.5, fill="faint", mono=True)
            if tag:
                pill(s, W - 64, y + 13, tag, fill="emberD", col="ember", size=10, anchor="end")
            s.text(W - 38, y + 27, "···", size=13, fill="dim", anchor="middle")
        s.end()
    y = 262
    s.rect(20, y - 4, W - 40, 34, rx=12, fill="panel2")
    s.text(36, y + 17, "Zen Provider Gateway", size=12.5, weight=500)
    pill(s, W - 34, y + 3, "Running", fill="sageD", col="sage", size=10, anchor="end")
    return s.render()


def skills():
    s = Svg("sk", W, 300, "Skills and Agent Plugins",
            "Skills on the current server: standalone, built-in and plugin-provided Skills with the agents that load them, "
            "and installed Agent Plugins with their Skills, Agents and copies.",
            states=2)
    seg(s, 20, 20, 240, ["Skills", "Agent Plugins"], [[1], [2]])
    s.rect(W - 128, 20, 108, 30, rx=15, fill="panel3")
    s.circle(W - 110, 34, 5, stroke="faint", sw=1.3)
    s.line(W - 106, 38, W - 103, 41, stroke="faint", sw=1.3)
    s.text(W - 96, 39.5, "Search", size=11.5, fill="faint")
    rows = [("release-notes", "Standalone", ["claude", "codex"]),
            ("ios-build", "Standalone", ["codex"]),
            ("brainstorming", "From superpowers", ["claude"]),
            ("frontend-design", "From superpowers", ["claude"]),
            ("skill-creator", "Built-in", ["claude"])]
    s.g("st s1")
    for i, (name, src, agents) in enumerate(rows):
        y = 66 + i * 45
        s.rect(20, y, W - 40, 39, rx=11, fill="panel2")
        s.rect(32, y + 10, 19, 19, rx=5, fill="sageD")
        s.path(f"M37 {y + 19.5}h9M41.5 {y + 15}v9", stroke="sage", sw=1.3)
        s.text(62, y + 18, name, size=12.5, weight=500, mono=True)
        s.text(62, y + 32, src, size=10.5, fill="faint" if src != "Built-in" else "dim")
        x = W - 32
        for a in reversed(agents):
            x -= pill(s, x, y + 10, a, size=10, mono=True, anchor="end") + 6
    s.end()
    s.g("st s2")
    plugins = [("superpowers", "14 Skills · 3 Agents · 2 copies", ["brainstorming", "frontend-design", "systematic-debugging"]),
               ("release-kit", "4 Skills · 1 command · 1 copy", ["changelog", "tag-release"])]
    y = 66
    for name, meta, sks in plugins:
        h = 44 + len(sks) * 24
        s.rect(20, y, W - 40, h, rx=12, fill="panel2")
        s.text(36, y + 24, name, size=13, weight=600, mono=True)
        s.text(W - 36, y + 24, meta, size=10.5, fill="faint", anchor="end")
        for j, sk in enumerate(sks):
            yy = y + 46 + j * 24
            s.line(40, yy - 9, 40, yy + 4, stroke="line2")
            s.line(40, yy - 2, 50, yy - 2, stroke="line2")
            s.text(58, yy + 2, sk, size=11.5, fill="dim", mono=True)
        y += h + 10
    s.end()
    return s.render()


def plugins():
    s = Svg("pl", W, 300, "Plugins connect outside services to Brain",
            "Linear, Notion, GitHub, Slack and Google Workspace connect to Brain through each service's own authorization, "
            "plus custom MCP and OpenAPI services. Read and search is included; making changes is a separate switch.",
            states=2,
            css=".pl .flow{stroke-dasharray:3 5;animation:pl-f 1.4s linear infinite}@keyframes pl-f{to{stroke-dashoffset:-16}}"
                "@media (prefers-reduced-motion:reduce){.pl .flow{animation:none}}")
    svc = [("Linear", "L"), ("Notion", "N"), ("GitHub", "G"), ("Slack", "S"), ("Google Workspace", "W"), ("MCP · OpenAPI", "{ }")]
    bx, by = 70, 150
    from brain import brain_mark
    brain_mark(s, bx, by, r=22)
    s.text(bx, by + 50, "Brain", size=12, weight=600, anchor="middle")
    for i, (name, g) in enumerate(svc):
        y = 28 + i * 42
        x = 150
        s.path(f"M{bx + 30} {by}C{bx + 60} {by},{x - 40} {y + 14},{x} {y + 14}", stroke="line2", sw=1)
        if i == 0:
            s.path(f"M{bx + 30} {by}C{bx + 60} {by},{x - 40} {y + 14},{x} {y + 14}", stroke="sage", sw=1.3, cls="st s2 flow")
        s.rect(x, y, 128, 28, rx=8, fill="panel2", stroke="line")
        s.text(x + 14, y + 18.5, g, size=10.5, fill="sage", mono=True, weight=500)
        s.text(x + 36, y + 18.5, name, size=11.5, fill="ink" if i < 5 else "dim")
    # What Brain can do
    px = 292
    s.rect(px, 28, W - px - 20, 244, rx=14, fill="panel2")
    s.text(px + 14, 52, "Linear", size=13, weight=600)
    s.text(px + 14, 68, "one connected account", size=10.5, fill="faint")
    s.text(px + 14, 98, "WHAT BRAIN CAN DO", size=9, fill="faint", mono=True, extra=' letter-spacing="1"')
    def sw(y, label, on_states):
        s.text(px + 14, y + 13, label[0], size=11.5)
        s.text(px + 14, y + 27, label[1], size=10, fill="faint")
        x = W - 34 - 30
        off = [v for v in (1, 2) if v not in on_states]
        on_cls = "st " + " ".join(f"s{v}" for v in on_states) if off else ""
        if off:
            off_cls = "st " + " ".join(f"s{v}" for v in off)
            s.rect(x, y + 3, 30, 18, rx=9, fill="panel3", cls=off_cls)
            s.circle(x + 9, y + 12, 7, fill="dim", cls=off_cls)
        s.rect(x, y + 3, 30, 18, rx=9, fill="sage2", cls=on_cls)
        s.circle(x + 21, y + 12, 7, fill="ink", cls=on_cls)
    sw(108, ("Read and search", "included"), (1, 2))
    sw(150, ("Make changes", "when you ask"), (2,))
    s.g("st s2")
    s.line(px + 14, 198, W - 34, 198, stroke="line")
    s.text(px + 14, 220, "Brain › Linear", size=10, fill="faint", mono=True)
    s.text(px + 14, 238, "create_issue", size=11.5, fill="sage", mono=True)
    s.text(px + 14, 254, "“Resume crash on Pixel 9”", size=10.5, fill="dim")
    s.end()
    s.g("st s1")
    s.line(px + 14, 198, W - 34, 198, stroke="line")
    s.text(px + 14, 222, "Changes stay off until", size=10.5, fill="faint")
    s.text(px + 14, 238, "you switch them on and", size=10.5, fill="faint")
    s.text(px + 14, 254, "confirm.", size=10.5, fill="faint")
    s.end()
    return s.render()


def spark(s, x, y, w, h, pts, col, cls=""):
    n = len(pts)
    d = "M" + " L".join(f"{x + w * i / (n - 1):.1f} {y + h - h * p:.1f}" for i, p in enumerate(pts))
    s.path(d, stroke=col, sw=1.4, cls=cls, extra=' stroke-linejoin="round"')


def resources():
    H = 560
    s = Svg("rs", W, H, "Resources on the current server",
            "CPU, memory, disk throughput and pressure trends, then the processes using them attributed to Workers, Brain, Docker or the user. "
            "An orphaned Worker shows as Residual with its original status.",
            states=4,
            css=".rs .wave{animation:rs-w 6s linear infinite}@keyframes rs-w{from{transform:translateX(0)}to{transform:translateX(-192px)}}"
                "@media (prefers-reduced-motion:reduce){.rs .wave{animation:none}}")
    s.text(20, 32, "homelab", size=13, weight=600)
    dot(s, 92, 28, "done", r=3.5)
    s.text(100, 32, "Pressure normal", size=11, fill="teal")
    s.text(W - 20, 32, "sampled 2s ago", size=10.5, fill="faint", anchor="end", mono=True)
    import math
    cards = [("CPU", "38%", "sage", 0.0), ("Memory", "61%", "sage", 1.3), ("Disk", "12 MB/s", "sage", 2.1), ("Pressure", "0.4%", "teal", 3.4)]
    cw = (W - 40 - 10) / 2
    s.add('<defs><clipPath id="rs-c0"><rect x="0" y="0" width="190" height="40"/></clipPath></defs>')
    for i, (lab, val, col, ph) in enumerate(cards):
        x = 20 + (i % 2) * (cw + 10)
        y = 50 + (i // 2) * 92
        s.rect(x, y, cw, 82, rx=12, fill="panel2")
        s.text(x + 14, y + 22, lab, size=11, fill="dim")
        s.text(x + cw - 14, y + 22, val, size=13, weight=600, anchor="end", mono=True)
        pts = [0.35 + 0.3 * math.sin(ph + j * 0.55) * (0.6 + 0.4 * math.sin(j * 1.7 + ph)) for j in range(28)]
        base = [0.25 + 0.2 * p for p in pts] if i == 3 else pts
        s.add(f'<g transform="translate({x + 14:g} {y + 32:g})" clip-path="url(#rs-c0)"><g class="wave">')
        spark(s, 0, 0, 192 * 2, 38, base + base, C[col])
        s.add("</g></g>")
    fy = 248
    chips = ["All", "Workers", "Brain", "Docker"]
    x = 20
    for i, c in enumerate(chips):
        w = len(c) * 7 + 26
        s.rect(x, fy, w, 26, rx=13, fill="panel3")
        s.rect(x, fy, w, 26, rx=13, fill="sageD", stroke="sage2", cls=f"st s{i + 1}")
        s.text(x + w / 2, fy + 17, c, size=11.5, fill="ink", anchor="middle")
        x += w + 8
    s.text(W - 20, fy + 17, "RSS ↓", size=10.5, fill="faint", anchor="end", mono=True)
    procs = [
        ("Android crash on resume", "Worker · claude", "1.2 GB", "84%", "w", "run"),
        ("Session list filters", "Worker · codex", "860 MB", "41%", "w", "run"),
        ("chrome", "User", "640 MB", "3%", "u", None),
        ("Brain", "Brain · codex", "420 MB", "6%", "b", "run"),
        ("postgres 16", "Docker · 3f9a…", "310 MB", "2%", "d", None),
        ("Changelog for 0.2", "Residual · done", "140 MB", "0%", "w", "done"),
        ("redis", "Docker · 81c2…", "48 MB", "1%", "d", None),
    ]
    filt = {1: "wubd", 2: "w", 3: "b", 4: "d"}
    for st, keys in filt.items():
        s.g(f"st s{st}")
        rows = [p for p in procs if p[4] in keys]
        for i, (name, owner, rss, cpu, _, status) in enumerate(rows):
            y = fy + 40 + i * 38
            s.rect(20, y, W - 40, 33, rx=9, fill="panel2")
            if status:
                dot(s, 34, y + 16.5, status, r=3.2)
            s.text(46, y + 14, name, size=12, weight=500)
            s.text(46, y + 27, owner, size=10, fill="faint", mono=True)
            s.text(W - 96, y + 21, rss, size=11.5, mono=True, anchor="end")
            s.text(W - 34, y + 21, cpu, size=11.5, fill="dim", mono=True, anchor="end")
        s.end()
    return s.render()


def stats():
    s = Svg("us", W, 300, "Usage and cost per model",
            "Usage per model on the current server. claude-opus-5-5 is a reported charge, gpt-5 is a reference estimate, "
            "and gpt-6-astra shows a dash because its price is not in the catalog.",
            states=2)
    s.text(20, 34, "≈ $182.20", size=24, weight=600, mono=True)
    s.text(20, 54, "48.1M tokens · 126 sessions · this month", size=11, fill="faint")
    rows = [("claude-opus-5-5", "$96.40", 0.53, "18.3M tokens · 41 sessions", "rep"),
            ("gpt-5", "≈ $61.10", 0.34, "14.9M tokens · 52 sessions", "est"),
            ("deepseek-v4-flash", "$24.70", 0.14, "6.2M tokens · 27 sessions", "rep"),
            ("gpt-6-astra", "—", 0.0, "8.7M tokens · 6 sessions", "unk")]
    s.g("st s1")
    for i, (m, amt, frac, sub, kind) in enumerate(rows):
        y = 76 + i * 54
        s.text(20, y + 14, m, size=12.5, mono=True, weight=500)
        s.text(W - 20, y + 14, amt, size=12.5, mono=True, anchor="end", fill="faint" if kind == "unk" else "ink")
        s.rect(20, y + 22, W - 40, 4, rx=2, fill="panel3")
        if frac:
            s.rect(20, y + 22, (W - 40) * frac, 4, rx=2, fill="sage" if kind == "rep" else "sage2")
        s.text(20, y + 42, sub, size=10.5, fill="faint")
    s.end()
    s.g("st s2")
    s.rect(20, 72, W - 40, 210, rx=14, fill="panel2")
    s.text(36, 98, "gpt-6-astra", size=14, mono=True, weight=600)
    s.text(W - 36, 98, "—", size=14, mono=True, anchor="end")
    kv = [("Cost", "Unknown"), ("Reason", "Price not in catalog"), ("Input", "8.1M tokens"), ("Output", "0.6M tokens"),
          ("Source", "codex history · refreshed 4m ago")]
    for i, (k_, v) in enumerate(kv):
        y = 124 + i * 26
        s.text(36, y, k_, size=11.5, fill="faint")
        s.text(136, y, v, size=11.5, fill="amber" if k_ == "Reason" else "ink", mono=k_ in ("Input", "Output"))
    s.line(36, 252, W - 36, 252, stroke="line")
    s.text(36, 270, "Unknown is not zero. Zen never borrows another model's price.", size=10.5, fill="dim")
    s.end()
    return s.render()


def calendar():
    s = Svg("ca", W, 300, "Calendar and scheduled actions",
            "A day in Calendar with an event, a reminder, a deadline and a daily scheduled action. "
            "When the scheduled action is due it becomes visible Work, runs in its own Session and posts the result to the Brain thread it came from.",
            states=2)
    x0, x1 = 30, W - 20
    hours = list(range(8, 21, 2))
    ty = 64
    def X(h):
        return x0 + (x1 - x0) * (h - 8) / 12
    s.text(20, 32, "Today", size=14, weight=600)
    s.text(W - 20, 32, "Europe/Berlin", size=10.5, fill="faint", anchor="end", mono=True)
    for h in hours:
        s.line(X(h), ty, X(h), 222, stroke="#1c1d18")
        s.text(X(h), ty - 8, f"{h:02d}", size=10, fill="faint", anchor="middle", mono=True)
    # now line
    s.line(X(13.4), ty, X(13.4), 222, stroke="ember", sw=1.2)
    s.circle(X(13.4), ty, 3, fill="ember")
    items = [
        (9, 9.6, 80, "Triage GitHub issues", "scheduled action · weekdays", "sage"),
        (10, 11, 120, "Design review", "event", "line2"),
        (12.5, 12.5, 160, "Renew the cert", "reminder", "amber"),
        (17, 17, 196, "Beta cut", "deadline", "ember"),
    ]
    for a, b, y, title, kind, col in items:
        if b > a:
            s.rect(X(a), y, X(b) - X(a), 26, rx=6, fill="sageD" if col == "sage" else "panel3", stroke=col)
        else:
            s.path(f"M{X(a)} {y + 4}l6 9-6 9-6-9Z", fill=col)
        tx = X(b) + 10 if b > a else X(a) + 12
        s.text(tx, y + 12, title, size=11.5, weight=500)
        s.text(tx, y + 24, kind, size=10, fill="faint", mono=True)
    # run chain
    s.g("st s2")
    s.rect(20, 236, W - 40, 50, rx=12, fill="panel2", stroke="sage2")
    steps = [("09:00 due", "faint"), ("Work w_57", "sage"), ("own Session", "sage"), ("result → Brain thread", "teal")]
    x = 34
    for i, (t, col) in enumerate(steps):
        s.text(x, 266, t, size=11, fill=col, mono=True)
        x += len(t) * 6.9 + 10
        if i < len(steps) - 1:
            s.text(x - 2, 266, "›", size=12, fill="faint")
            x += 12
    s.end()
    s.g("st s1")
    s.text(20, 262, "Add items in the app, or ask Brain in chat.", size=11, fill="faint")
    s.text(20, 280, "A scheduled action runs as visible Work, never silently.", size=11, fill="faint")
    s.end()
    return s.render()


def services():
    s = Svg("sv", W, 300, "Services your agents started",
            "The Services sheet lists listening ports owned by Sessions or persistent units, grouped by project. "
            "Port 5173 can be made public with a temporary Quick Tunnel.",
            states=2)
    s.text(20, 32, "Services", size=14, weight=600)
    s.text(W - 20, 32, "3 ports", size=10.5, fill="faint", anchor="end", mono=True)
    groups = [("~/zen", [("8081", "metro", "Session · onboarding", "lan")]),
              ("~/site", [("5173", "vite", "Session · docs-sync", "lan"),
                          ("5432", "postgres", "Persistent · pg.service", "local")])]
    y = 50
    for g, rows in groups:
        s.text(20, y + 12, g, size=10.5, fill="faint", mono=True)
        y += 20
        for port, proc, src, bind in rows:
            s.rect(20, y, W - 40, 40, rx=10, fill="panel2")
            s.text(34, y + 25, port, size=14, mono=True, weight=600)
            s.text(84, y + 18, proc, size=12, weight=500)
            s.text(84, y + 32, src, size=10, fill="faint", mono=True)
            if bind == "local":
                pill(s, W - 34, y + 10, "127.0.0.1", size=10, mono=True, anchor="end")
            elif port == "5173":
                pill(s, W - 34, y + 10, "Public", fill="sageD", col="sage", size=10, anchor="end")
                s.out[-2] = s.out[-2].replace("<rect ", '<rect class="st s2" ', 1)
                s.out[-1] = s.out[-1].replace("<text ", '<text class="st s2" ', 1)
            y += 46
        y += 4
    s.g("st s2")
    s.rect(20, y + 2, W - 40, 44, rx=10, fill="panel", stroke="sage2")
    s.text(34, y + 20, "Quick Tunnel · 5173", size=10.5, fill="faint", mono=True)
    s.text(34, y + 36, "https://<random>.trycloudflare.com", size=11.5, fill="sage", mono=True)
    s.end()
    s.g("st s1")
    s.text(20, y + 24, "Loopback stays local. Zen never invents a LAN URL.", size=11, fill="faint")
    s.end()
    return s.render()


def alerts():
    s = Svg("al", W, 300, "Alerts and Telegram",
            "Push alerts arrive only when a Session is blocked, failed or finished. Telegram is another channel to the same Brain.",
            states=0)
    s.text(20, 30, "PUSH", size=9.5, fill="faint", mono=True, extra=' letter-spacing="1.2"')
    notes = [("need", "session-auth needs you", "Approve running the migration?"),
             ("fail", "flaky-tests failed", "3 of 212 tests still fail on CI"),
             ("done", "ios-crash finished", "Fixed the resume race; tests pass")]
    for i, (k_, t, b) in enumerate(notes):
        y = 42 + i * 56
        s.rect(20, y, 230, 48, rx=14, fill="panel2")
        s.rect(30, y + 10, 20, 20, rx=5, fill="#151a18")
        dot(s, 40, y + 20, k_, r=3.5)
        s.text(58, y + 20, t, size=11.5, weight=600)
        s.text(58, y + 36, b, size=10, fill="dim")
    s.text(20, 230, "Nothing for routine progress.", size=11, fill="faint")
    s.text(20, 248, "Silent while you watch that Session.", size=11, fill="faint")
    # Telegram
    tx = 270
    s.text(tx, 30, "TELEGRAM", size=9.5, fill="faint", mono=True, extra=' letter-spacing="1.2"')
    s.rect(tx, 42, W - tx - 20, 216, rx=14, fill="panel2")
    s.rect(W - 20 - 120, 58, 106, 28, rx=12, fill="sageD")
    s.text(W - 20 - 67, 76, "where are we?", size=11, fill="ink", anchor="middle")
    lines = ["2 Workers running.", "session-auth waits on", "your approval. ios-crash", "is done and accepted."]
    s.rect(tx + 12, 98, 160, 86, rx=12, fill="panel3")
    for i, ln in enumerate(lines):
        s.text(tx + 24, 118 + i * 17, ln, size=11, fill="ink")
    s.text(tx + 14, 204, "Brain · same thread", size=10, fill="faint", mono=True)
    s.text(tx + 14, 220, "as the app", size=10, fill="faint", mono=True)
    return s.render()


ALL = {"models": models, "skills": skills, "plugins": plugins, "resources": resources, "stats": stats,
       "calendar": calendar, "services": services, "alerts": alerts}

if __name__ == "__main__":
    import sys
    sys.stdout.write(ALL[sys.argv[1]]())
