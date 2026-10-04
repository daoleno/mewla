#!/usr/bin/env python3
# Generates site/assets/switch.svg, shared by the landing page and README.
# Run from the repo root: python3 scripts/site-svg/switch.py > site/assets/switch.svg
import math
import sys
from html import escape as e

SANS = 'Geist,ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif'
MONO = '"Geist Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace'

EX = [
    # id, display, color, model, levels, selected level
    ("codex", "Codex", "#4e6ba2", "gpt-6-astra", ["none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"], "high"),
    ("claude", "Claude Code", "#b47c6c", "claude-opus-5-5", ["low", "medium", "high", "xhigh", "max"], "high"),
    ("pi", "Pi", "#559e72", "opencode-go/deepseek-v4.1-flash", ["off", "minimal", "low", "medium", "high", "xhigh", "max"], "low"),
    ("grok", "Grok", "#3f4441", None, None, None),
    ("agent", "Cursor", "#8a7c66", None, None, None),
    ("opencode", "OpenCode", "#c08a3e", None, None, None),
]
N = len(EX)
PERIOD = 18.0
SLOT = PERIOD / N

out = []
w = out.append

def cyc(prefix, offset):
    rules = []
    for k, ex in enumerate(EX):
        d = PERIOD - SLOT * k + offset
        rules.append(f".sw:not(.is-live) .sw-{prefix}-{ex[0]}{{animation-delay:-{d % PERIOD:.2f}s}}")
    return "".join(rules)

live_h = ",".join(f'.sw.is-live[data-host="{ex[0]}"] .sw-h-{ex[0]}' for ex in EX)
live_e = ",".join(f'.sw.is-live[data-exec="{ex[0]}"] .sw-e-{ex[0]}' for ex in EX)

w(f'''<svg xmlns="http://www.w3.org/2000/svg" class="sw" viewBox="0 0 960 520" data-n-viewbox="0 0 420 950" data-host="codex" data-exec="codex" role="img" aria-labelledby="sw-title sw-desc">
<title id="sw-title">Switch the agent behind Brain, and pick a model for each Worker</title>
<desc id="sw-desc">Left: Brain sits inside a ring of six agent clients. zen brain use moves Brain to another client while its thread and history stay. Right: each Worker gets its own executor, model and reasoning level. codex, claude and pi accept a reasoning level from their own scale; grok, Cursor agent and opencode take a model only. A running Worker keeps the executor it started with.</desc>
<style>
.sw{{--ink:#171a18;--ink2:#4a504c;--mute:#7a817c;--line:#e4e0d6;--sage:#559e72;--sage7:#2a5f41}}
.sw text{{white-space:pre;font-family:{SANS};fill:var(--ink)}}
.sw .m,.sw .m text{{font-family:{MONO}}}
.sw .mute{{fill:var(--mute)}}.sw .ink2{{fill:var(--ink2)}}.sw .inv{{fill:#fff}}.sw .sage{{fill:var(--sage7)}}
.sw .cap{{font-size:11px;letter-spacing:.08em;text-transform:uppercase;fill:var(--mute)}}
.sw .card{{fill:#fff;stroke:var(--line)}}
.sw [data-pick]{{cursor:pointer}}
.sw .sw-h,.sw .sw-e{{opacity:0}}.sw .sw-h-codex,.sw .sw-e-codex{{opacity:1}}
.sw.is-live .sw-h,.sw.is-live .sw-e{{opacity:0;transition:opacity .3s ease}}
{live_h},{live_e}{{opacity:1}}
.sw.is-live [data-pick]:hover .seat,.sw.is-live [data-pick]:focus-visible .seat{{stroke:var(--sage)}}
.sw [data-pick]:focus{{outline:none}}
@media (prefers-reduced-motion:no-preference){{
.sw:not(.is-live) .sw-h,.sw:not(.is-live) .sw-e{{animation:sw-cycle {PERIOD:.0f}s infinite both}}
{cyc("h", 0)}
{cyc("e", 1.5)}
.sw .orbit{{animation:sw-flow 4s linear infinite}}
}}
@keyframes sw-cycle{{0%{{opacity:0}}2%,15.5%{{opacity:1}}16.66%,100%{{opacity:0}}}}
@keyframes sw-flow{{to{{stroke-dashoffset:-90}}}}
</style>
<rect class="sw-bg" width="100%" height="100%" rx="22" fill="#fbfaf7"/>
''')

# ---------------- Brain host orbit ----------------
CX, CY, RX, RY = 240, 262, 172, 142
w('<g class="sw-host" data-n-transform="translate(-30 0)">')
w('<text x="40" y="52" class="cap">Brain host</text>')
w(f'<ellipse class="orbit" cx="{CX}" cy="{CY}" rx="{RX}" ry="{RY}" fill="none" stroke="#dcd7cc" stroke-width="1.5" stroke-dasharray="2 7"/>')
seats = []
for k, ex in enumerate(EX):
    a = math.radians(-90 + 60 * k)
    sx, sy = CX + RX * math.cos(a), CY + RY * math.sin(a)
    seats.append((sx, sy))
# links (one per host state)
for k, ex in enumerate(EX):
    sx, sy = seats[k]
    # stop at the core edge and the seat edge
    dx, dy = sx - CX, sy - CY
    d = math.hypot(dx, dy)
    x1, y1 = CX + dx / d * 58, CY + dy / d * 58
    x2, y2 = sx - dx / d * 24, sy - dy / d * 22
    w(f'<path class="sw-h sw-h-{ex[0]}" d="M{x1:.1f} {y1:.1f}L{x2:.1f} {y2:.1f}" stroke="#559e72" stroke-width="2.5" stroke-linecap="round"/>')
# core
w(f'<circle cx="{CX}" cy="{CY}" r="54" fill="#f1f9f3" stroke="#559e72" stroke-width="1.5"/>')
w(f'<g transform="translate({CX-33} {CY-41}) scale(.064)" fill="none" stroke="#2a5f41" stroke-linecap="round" stroke-linejoin="round">'
  '<path d="M790 372C724 252 590 209 461 244C334 279 252 393 266 524C282 675 411 778 558 760C650 749 731 697 781 620" stroke-width="64"/>'
  '<path d="M334 430C470 352 622 334 665 371C694 396 671 440 601 495C486 586 401 663 437 705C472 747 606 730 722 801" stroke-width="78"/></g>')
w(f'<text x="{CX}" y="{CY+30}" text-anchor="middle" font-size="12.5" font-weight="600" class="sage">Brain</text>')
# seats
for k, ex in enumerate(EX):
    sx, sy = seats[k]
    wd = 104
    x, y = sx - wd / 2, sy - 17
    w(f'<g data-pick="host" data-value="{ex[0]}" aria-label="Run Brain on {ex[0]}">')
    w(f'<rect class="seat" x="{x:.1f}" y="{y:.1f}" width="{wd}" height="34" rx="17" fill="#fff" stroke="#e4e0d6" stroke-width="1.5"/>')
    w(f'<circle cx="{x+18:.1f}" cy="{sy:.1f}" r="4.5" fill="{ex[2]}"/>')
    w(f'<text x="{x+30:.1f}" y="{sy+4.5:.1f}" font-size="13" class="m">{ex[0]}</text>')
    w(f'<g class="sw-h sw-h-{ex[0]}"><rect x="{x:.1f}" y="{y:.1f}" width="{wd}" height="34" rx="17" fill="#1f2421"/>'
      f'<circle cx="{x+18:.1f}" cy="{sy:.1f}" r="4.5" fill="{ex[2]}" stroke="#fff" stroke-width="1.2"/>'
      f'<text x="{x+30:.1f}" y="{sy+4.5:.1f}" font-size="13" class="m inv">{ex[0]}</text></g>')
    w('</g>')
# command + caption
w('<rect x="70" y="446" width="340" height="40" rx="12" fill="#1c211e"/>')
for ex in EX:
    w(f'<text x="88" y="471" font-size="13" class="m sw-h sw-h-{ex[0]}"><tspan style="fill:#8fa597">$</tspan><tspan style="fill:#e9efe9"> zen brain use </tspan><tspan style="fill:#a8e3bd">{ex[0]}</tspan></text>')
w('<text x="240" y="508" text-anchor="middle" font-size="12.5" class="ink2">The thread and its history stay.</text>')
w('</g>')

# ---------------- per-Worker picker ----------------
X0, Y0, W0 = 520, 70, 400
w('<g class="sw-worker" data-n-transform="translate(-510 482)">')
w(f'<text x="{X0}" y="52" class="cap">Each Worker</text>')
w(f'<rect class="card" x="{X0+.5}" y="{Y0+.5}" width="{W0}" height="300" rx="16"/>')
w(f'<text x="{X0+20}" y="{Y0+30}" font-size="12" class="mute">Executor</text>')
tw = (W0 - 40 - 16) / 3
for k, ex in enumerate(EX):
    col, row = k % 3, k // 3
    x = X0 + 20 + col * (tw + 8)
    y = Y0 + 42 + row * 42
    w(f'<g data-pick="exec" data-value="{ex[0]}" aria-label="Spawn a Worker on {ex[0]}">')
    w(f'<rect class="seat" x="{x:.1f}" y="{y}" width="{tw:.1f}" height="34" rx="10" fill="#fbfaf7" stroke="#e4e0d6" stroke-width="1.5"/>')
    w(f'<circle cx="{x+16:.1f}" cy="{y+17}" r="4.5" fill="{ex[2]}"/>')
    w(f'<text x="{x+28:.1f}" y="{y+21.5}" font-size="12.5" class="m">{ex[0]}</text>')
    w(f'<g class="sw-e sw-e-{ex[0]}"><rect x="{x:.1f}" y="{y}" width="{tw:.1f}" height="34" rx="10" fill="#f1f9f3" stroke="#2a5f41" stroke-width="2"/>'
      f'<circle cx="{x+16:.1f}" cy="{y+17}" r="4.5" fill="{ex[2]}"/><text x="{x+28:.1f}" y="{y+21.5}" font-size="12.5" font-weight="600" class="m">{ex[0]}</text></g>')
    w('</g>')
# model field
my = Y0 + 150
w(f'<text x="{X0+20}" y="{my}" font-size="12" class="mute">Model</text>')
w(f'<rect x="{X0+20.5}" y="{my+10.5}" width="{W0-40}" height="36" rx="10" fill="#fbfaf7" stroke="#e4e0d6"/>')
for ex in EX:
    model = ex[3] or "client default"
    cls = "m" if ex[3] else "m mute"
    w(f'<text x="{X0+34}" y="{my+33}" font-size="12.5" class="{cls} sw-e sw-e-{ex[0]}">{e(model)}</text>')
# reasoning ladder
ry = Y0 + 230
w(f'<text x="{X0+20}" y="{ry}" font-size="12" class="mute">Reasoning</text>')
LW = W0 - 40
for ex in EX:
    levels, sel = ex[4], ex[5]
    w(f'<g class="sw-e sw-e-{ex[0]}">')
    if not levels:
        w(f'<rect x="{X0+20.5}" y="{ry+10.5}" width="{LW}" height="34" rx="10" fill="none" stroke="#d8d3c8" stroke-dasharray="3 4"/>')
        w(f'<text x="{X0+20+LW/2}" y="{ry+32}" text-anchor="middle" font-size="12" class="mute">{ex[0]} takes a model only</text>')
    else:
        n = len(levels)
        gap = 3
        sw_ = (LW - gap * (n - 1)) / n
        for i, lv in enumerate(levels):
            x = X0 + 20 + i * (sw_ + gap)
            on = lv == sel
            below = levels.index(sel) >= i
            fill = "#1f2421" if on else ("#dcf2e3" if below else "#f3f1ec")
            w(f'<rect x="{x:.1f}" y="{ry+10}" width="{sw_:.1f}" height="34" rx="7" fill="{fill}"/>')
            cls = "m inv" if on else ("m sage" if below else "m mute")
            fs = 10 if n > 6 else 11.5
            w(f'<text x="{x+sw_/2:.1f}" y="{ry+31}" text-anchor="middle" font-size="{fs}" class="{cls}">{lv}</text>')
    w('</g>')
# running worker keeps its executor
ky = Y0 + 318
w(f'<rect class="card" x="{X0+.5}" y="{ky+.5}" width="{W0}" height="58" rx="14"/>')
w(f'<circle cx="{X0+24}" cy="{ky+29}" r="5" fill="#559e72"/><circle cx="{X0+24}" cy="{ky+29}" r="9" fill="none" stroke="#559e72" stroke-opacity=".3" stroke-width="3"/>')
w(f'<text x="{X0+42}" y="{ky+25}" font-size="13" font-weight="600">Session auth</text>')
w(f'<text x="{X0+42}" y="{ky+43}" font-size="11.5" class="ink2">running · keeps codex until it ends</text>')
w(f'<g transform="translate({X0+W0-36} {ky+19})" fill="none" stroke="#7a817c" stroke-width="1.6" stroke-linejoin="round"><rect x="0" y="8" width="16" height="12" rx="3"/><path d="M3.5 8V5a4.5 4.5 0 0 1 9 0v3"/></g>')
w('</g>')
w('</svg>\n')
sys.stdout.write("\n".join(out))
