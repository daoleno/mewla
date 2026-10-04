#!/usr/bin/env python3
# Generates site/assets/dispatch.svg, shared by the landing page and README.
# Run from the repo root: python3 scripts/site-svg/dispatch.py > site/assets/dispatch.svg
import sys
from html import escape as e

N = 5
PERIOD = 18.0
SLOT = PERIOD / N

SANS = 'Geist,ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif'
MONO = '"Geist Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace'

def st(k):
    return f"dp-s dp-s{k}"

out = []
w = out.append

delays = "".join(
    f".dp:not(.is-live) .dp-s{k}{{animation-delay:-{PERIOD - SLOT*(k-1):.1f}s}}" for k in range(2, N + 1)
)
live = ",".join(f'.dp.is-live[data-state="{k}"] .dp-s{k}' for k in range(1, N + 1))

w(f'''<svg xmlns="http://www.w3.org/2000/svg" class="dp" viewBox="0 0 960 600" data-live-viewbox="0 0 960 536" data-n-viewbox="0 0 420 700" data-state="1" role="img" aria-labelledby="dp-title dp-desc">
<title id="dp-title">Brain plans, delegates, reviews and accepts Work</title>
<desc id="dp-desc">You give Brain one goal from your phone. Brain records three Work items and hands each to a visible Worker Session on a different agent. Workers check in while they run. Brain reviews each result, sends one Worker back with a follow-up, and accepts the Work only after reading the diffs and checks. Your phone gets one notification when it is done.</desc>
<style>
.dp{{--ink:#171a18;--ink2:#4a504c;--mute:#7a817c;--line:#e4e0d6;--sage:#559e72;--sage7:#2a5f41;--sage1:#dcf2e3;--sage0:#f1f9f3;--clay:#b47c6c;--clay1:#fbe7e1;--well:#f6f5f1}}
.dp text{{white-space:pre;font-family:{SANS};fill:var(--ink)}}
.dp .m,.dp .m text{{font-family:{MONO}}}
.dp .mute{{fill:var(--mute)}}.dp .ink2{{fill:var(--ink2)}}.dp .sage{{fill:var(--sage7)}}.dp .clay{{fill:#8f5a4b}}
.dp .cap{{font-size:11px;letter-spacing:.08em;text-transform:uppercase;fill:var(--mute)}}
.dp .card{{fill:#fff;stroke:var(--line)}}
.dp .ghost{{fill:#fff;stroke:var(--line);stroke-dasharray:4 5}}
.dp .wire{{fill:none;stroke:var(--sage);stroke-width:2;stroke-linecap:round;stroke-dasharray:3 6}}
.dp .wire.back{{stroke:var(--sage7)}}
.dp .wire.follow{{stroke:var(--clay)}}
.dp-s{{opacity:0}}.dp-s1{{opacity:1}}
.dp.is-live .dp-s{{opacity:0;transition:opacity .35s ease}}
{live}{{opacity:1}}
.dp.is-live .dp-track{{display:none}}
@media (prefers-reduced-motion:no-preference){{
.dp:not(.is-live) .dp-s{{animation:dp-cycle {PERIOD:.0f}s infinite both}}
{delays}
.dp .wire{{animation:dp-flow 1.2s linear infinite}}
.dp .wire.back{{animation-direction:reverse}}
.dp .pulse{{animation:dp-pulse 3.2s ease-out infinite;transform-origin:center;transform-box:fill-box}}
}}
@keyframes dp-cycle{{0%{{opacity:0}}2.5%,18%{{opacity:1}}20%,100%{{opacity:0}}}}
@keyframes dp-flow{{to{{stroke-dashoffset:-18}}}}
@keyframes dp-pulse{{0%{{opacity:.5;transform:scale(1)}}100%{{opacity:0;transform:scale(1.45)}}}}
</style>
<rect class="dp-bg" width="100%" height="100%" rx="22" fill="#fbfaf7"/>
''')

# ---------- phone ----------
w('<g class="dp-phone" data-n-visibility="hidden">')
w('<rect x="32" y="36" width="212" height="484" rx="34" fill="#1f2421"/>')
w('<rect x="41" y="45" width="194" height="466" rx="26" fill="#fff"/>')
w('<rect x="112" y="53" width="52" height="14" rx="7" fill="#1f2421"/>')
w('<text x="62" y="65" font-size="11" font-weight="600">9:41</text>')
w('<text x="110" y="96" font-size="13" font-weight="700">Brain</text><text x="160" y="96" font-size="13" class="mute">Sessions</text>')
w('<path d="M108 103h42" stroke="#2a5f41" stroke-width="2" stroke-linecap="round"/>')
w('<path d="M41 111.5h194" stroke="#ece9e1"/>')
w('<rect x="84" y="124" width="140" height="58" rx="15" fill="#dcf2e3"/>')
for i, line in enumerate(["Ship the 0.2 beta", "while I’m out", "tonight."]):
    w(f'<text x="97" y="{145 + i*16}" font-size="12">{e(line)}</text>')
phone_msgs = {
    1: ["Planning. Three Work", "items, in order."],
    2: ["Delegated. Each Worker", "is a Session you can", "open from Sessions."],
    3: ["Workers are checking in.", "I’ll review results", "as they land."],
    4: ["Reviewing. The Android", "Worker goes back for", "two flaky tests."],
    5: ["Accepted all three after", "reading diffs and", "checks."],
}
for k, lines in phone_msgs.items():
    w(f'<g class="{st(k)}">')
    w(f'<circle cx="60" cy="207" r="4" fill="#559e72"/><text x="70" y="211" font-size="10.5" class="mute">Brain</text>')
    for i, line in enumerate(lines):
        w(f'<text x="56" y="{232 + i*17}" font-size="12.5">{e(line)}</text>')
    w('</g>')
# work cards in phone, state dependent status
items = [("Session auth", "codex"), ("Android crash", "claude"), ("Release notes", "pi")]
status = {
    1: ["planned", "planned", "planned"],
    2: ["running", "running", "running"],
    3: ["running", "running", "running"],
    4: ["result", "follow-up", "result"],
    5: ["done", "done", "done"],
}
for i, (title, ex) in enumerate(items):
    y = 300 + i * 46
    w(f'<rect x="52.5" y="{y+.5}" width="172" height="38" rx="10" fill="#fbfaf7" stroke="#ece9e1"/>')
    w(f'<text x="64" y="{y+16}" font-size="11.5" font-weight="600">{e(title)}</text>')
    w(f'<text x="64" y="{y+30}" font-size="10" class="m mute">{ex}</text>')
    for k in range(1, N + 1):
        s = status[k][i]
        cls = "sage" if s in ("done", "result") else ("clay" if s == "follow-up" else "mute")
        w(f'<text x="214" y="{y+24}" font-size="10.5" text-anchor="end" class="{cls} {st(k)}">{s}</text>')
w('<rect x="52.5" y="468.5" width="172" height="30" rx="15" fill="#fff" stroke="#e4e0d6"/>')
w('<text x="68" y="488" font-size="12" class="mute">Ask Brain</text>')
# notification on accept
w(f'<g class="{st(5)}"><rect x="48" y="52" width="180" height="46" rx="13" fill="#1f2421"/>'
  '<circle cx="66" cy="75" r="8" fill="#559e72"/><path d="M62 75l3 3 5-6" fill="none" stroke="#fff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>'
  '<text x="82" y="71" font-size="11" font-weight="600" style="fill:#fff">Brain finished</text>'
  '<text x="82" y="87" font-size="10.5" style="fill:#c9d3cc">Release 0.2 is ready for you</text></g>')
w('</g>')

# phone -> Brain link
w('<g data-n-visibility="hidden"><path d="M244 150C282 150 290 120 318 120" fill="none" stroke="#c9c4b8" stroke-width="1.6" stroke-dasharray="2 5"/>'
  '<text x="281" y="172" text-anchor="middle" font-size="10" class="mute">signed</text></g>')

# ---------- Brain + plan ----------
w('<g class="dp-brain" data-n-transform="translate(-260 0)">')
w('<circle class="pulse" cx="360" cy="120" r="40" fill="none" stroke="#559e72" stroke-width="1.5"/>')
w('<circle cx="360" cy="120" r="40" fill="#f1f9f3" stroke="#559e72" stroke-width="1.5"/>')
w('<g transform="translate(333 93) scale(.053)" fill="none" stroke="#2a5f41" stroke-linecap="round" stroke-linejoin="round">'
  '<path d="M790 372C724 252 590 209 461 244C334 279 252 393 266 524C282 675 411 778 558 760C650 749 731 697 781 620" stroke-width="64"/>'
  '<path d="M334 430C470 352 622 334 665 371C694 396 671 440 601 495C486 586 401 663 437 705C472 747 606 730 722 801" stroke-width="78"/></g>')
w('<text x="360" y="181" text-anchor="middle" font-size="14" font-weight="600">Brain</text>')
w('</g>')

w('<g class="dp-plan" data-n-transform="translate(-72 -170)">')
w('<rect class="card" x="268.5" y="190.5" width="184" height="210" rx="14"/>')
w('<text x="286" y="216" class="cap">Work</text>')
glyph_kind = {
    1: ["plan", "plan", "plan"],
    2: ["run", "run", "run"],
    3: ["run", "run", "run"],
    4: ["review", "follow", "review"],
    5: ["done", "done", "done"],
}
for i, (title, ex) in enumerate(items):
    y = 248 + i * 52
    w(f'<text x="306" y="{y+4}" font-size="13" font-weight="600">{e(title)}</text>')
    w(f'<text x="306" y="{y+21}" font-size="10.5" class="m mute">w_3{i+1} · {ex}</text>')
    for k in range(1, N + 1):
        kind = glyph_kind[k][i]
        cx, cy = 291, y
        if kind == "plan":
            g = f'<circle cx="{cx}" cy="{cy}" r="5.5" fill="#fff" stroke="#a8ada9" stroke-width="1.5"/>'
        elif kind == "run":
            g = f'<circle cx="{cx}" cy="{cy}" r="5.5" fill="#dcf2e3" stroke="#559e72" stroke-width="1.5"/><circle cx="{cx}" cy="{cy}" r="2.4" fill="#559e72"/>'
        elif kind == "review":
            g = f'<circle cx="{cx}" cy="{cy}" r="5.5" fill="#559e72"/><path d="M{cx-3} {cy}h6" stroke="#fff" stroke-width="1.6" stroke-linecap="round"/>'
        elif kind == "follow":
            g = f'<circle cx="{cx}" cy="{cy}" r="5.5" fill="#b47c6c"/><path d="M{cx-2.5} {cy-2.5}l5 2.5-5 2.5" fill="none" stroke="#fff" stroke-width="1.4" stroke-linejoin="round"/>'
        else:
            g = f'<circle cx="{cx}" cy="{cy}" r="6" fill="#2a5f41"/><path d="M{cx-3} {cy}l2.2 2.2 4-4.4" fill="none" stroke="#fff" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>'
        w(f'<g class="{st(k)}">{g}</g>')
w('</g>')

# ---------- wires plan -> workers ----------
ys_from = [252, 304, 356]
ys_to = [100, 250, 400]
w('<g class="dp-wires" data-n-visibility="hidden">')
for a, b in zip(ys_from, ys_to):
    w(f'<path d="M452 {a}C500 {a} 500 {b} 548 {b}" fill="none" stroke="#ebe7de" stroke-width="2"/>')
for k in range(2, N):
    for i, (a, b) in enumerate(zip(ys_from, ys_to)):
        cls = "wire"
        if k == 3:
            cls = "wire back"
        if k == 4:
            cls = "wire follow" if i == 1 else "wire back"
        w(f'<path class="{cls} {st(k)}" d="M452 {a}C500 {a} 500 {b} 548 {b}"/>')
w('</g>')

# ---------- workers ----------
workers = [
    ("Session auth", "codex", "#4e6ba2", "gpt-6-astra · high"),
    ("Android crash on resume", "claude", "#b47c6c", "claude-opus-5-5 · high"),
    ("Release notes", "pi", "#559e72", "opencode-go/deepseek-v4.1-flash · low"),
]
body = {
    1: [("waiting for Brain", ""), ("waiting for Brain", ""), ("waiting for Brain", "")],
    2: [("spawned · fence 1", "reading daemon/auth"), ("spawned · fence 1", "reproducing on device"), ("spawned · fence 1", "collecting merged changes")],
    3: [("progress · verifying", "38 of 42 checks green"), ("progress · working", "crash reproduced, fixing"), ("progress · working", "2 of 3 sections drafted")],
    4: [("result · diff +128 −41", "42 of 42 checks green"), ("result · 2 tests flaky", "Brain: fix the two flaky cases"), ("result · notes drafted", "links verified")],
    5: [("accepted by Brain", "diff and checks reviewed"), ("fence 2 · 44 of 44 green", "accepted by Brain"), ("accepted by Brain", "ready to publish")],
}
foot = {
    1: ["queued", "queued", "queued"],
    2: ["running", "running", "running"],
    3: ["checked in 40s ago", "checked in 1m ago", "checked in 20s ago"],
    4: ["result in", "follow-up sent", "result in"],
    5: ["done", "done", "done"],
}
lease = {1: [0, 0, 0], 2: [.2, .2, .2], 3: [.66, .5, .8], 4: [1, .3, 1], 5: [1, 1, 1]}
w('<g class="dp-workers" data-n-transform="translate(-528 214)">')
for i, (title, ex, color, model) in enumerate(workers):
    y = 36 + i * 150
    w(f'<rect class="card" x="548.5" y="{y+.5}" width="380" height="128" rx="14"/>')
    w(f'<g class="{st(1)}"><rect class="ghost" x="548.5" y="{y+.5}" width="380" height="128" rx="14"/></g>')
    w(f'<text x="566" y="{y+28}" font-size="14" font-weight="600">{e(title)}</text>')
    w(f'<circle cx="{906 - len(ex)*7.6 - 12}" cy="{y+23}" r="4.5" fill="{color}"/><text x="910" y="{y+28}" text-anchor="end" font-size="12.5" class="m">{ex}</text>')
    w(f'<text x="566" y="{y+46}" font-size="11" class="m mute">{e(model)}</text>')
    w(f'<rect x="564" y="{y+56}" width="348" height="42" rx="8" fill="#f6f5f1"/>')
    for k in range(1, N + 1):
        l1, l2 = body[k][i]
        c2 = "clay" if (k == 4 and i == 1) else "ink2"
        w(f'<g class="{st(k)} m" font-size="11.5"><text x="576" y="{y+73}" class="{"mute" if k == 1 else "sage"}">{e(l1)}</text>'
          f'<text x="576" y="{y+90}" class="{c2}">{e(l2)}</text></g>')
        f = foot[k][i]
        fc = "clay" if f == "follow-up sent" else ("sage" if f in ("done", "result in") else "mute")
        w(f'<text x="566" y="{y+116}" font-size="11" class="{fc} {st(k)}">{e(f)}</text>')
        frac = lease[k][i]
        if frac:
            col = "#b47c6c" if (k == 4 and i == 1) else "#559e72"
            w(f'<rect class="{st(k)}" x="752" y="{y+111}" width="{160*frac:.0f}" height="4" rx="2" fill="{col}"/>')
    w(f'<rect x="752" y="{y+111}" width="160" height="4" rx="2" fill="none" stroke="#e4e0d6"/>')
w('</g>')

# ---------- lifecycle track ----------
steps = ["Plan", "Delegate", "Check in", "Review", "Accept"]
w('<g class="dp-track" data-n-visibility="hidden">')
x0, x1 = 300, 900
w(f'<path d="M{x0} 552H{x1}" stroke="#e4e0d6" stroke-width="2"/>')
for i, s in enumerate(steps):
    x = x0 + i * (x1 - x0) / (len(steps) - 1)
    w(f'<circle cx="{x:.0f}" cy="552" r="7" fill="#fff" stroke="#c9c4b8" stroke-width="1.5"/>')
    w(f'<text x="{x:.0f}" y="580" text-anchor="middle" font-size="12" class="mute">{s}</text>')
    w(f'<g class="{st(i+1)}"><circle cx="{x:.0f}" cy="552" r="7" fill="#2a5f41"/><circle cx="{x:.0f}" cy="552" r="13" fill="none" stroke="#559e72" stroke-opacity=".35" stroke-width="4"/>'
      f'<rect x="{x-44:.0f}" y="566" width="88" height="20" fill="#fbfaf7"/><text x="{x:.0f}" y="580" text-anchor="middle" font-size="12" font-weight="700">{s}</text></g>')
w('<text x="32" y="557" class="cap">Lifecycle</text>')
w('</g>')
w('</svg>\n')
sys.stdout.write("\n".join(out))
