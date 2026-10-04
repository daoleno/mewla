#!/usr/bin/env python3
# Generates site/assets/hero.svg, shared by the landing page and README.
# Run from the repo root: python3 scripts/site-svg/hero.py > site/assets/hero.svg
import math
import sys
from html import escape as e

SANS = 'Geist,ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif'
MONO = '"Geist Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace'
COL = {"codex": "#4e6ba2", "claude": "#b47c6c", "pi": "#559e72", "grok": "#3f4441", "agent": "#8a7c66", "opencode": "#c08a3e"}

TILES = [
    ("onboarding", "claude"),
    ("session-auth", "codex"),
    ("ios-crash", "claude"),
    ("changelog", "pi"),
    ("parser-port", "grok"),
    ("flaky-tests", "codex"),
    ("deps-bump", "pi"),
    ("docs-sync", "opencode"),
]
# per tile: status cycle offset (s) and which statuses it shows in reduced motion
STATIC = ["work", "work", "you", "done", "work", "work", "done", "work"]

TW, TH = 146, 52
BX, BY = 466, 272          # Brain centre (wide)
NBX, NBY = 210, 258        # Brain centre (narrow)

def wide_pos(i):
    a = math.radians(-90 + 45 * i + 22.5)
    return BX + 200 * math.cos(a), BY + 196 * math.sin(a)

def narrow_pos(i):
    side = 0 if i in (5, 6, 7, 4) else 1
    order = {5: 0, 6: 1, 7: 2, 4: 3, 0: 0, 1: 1, 2: 2, 3: 3}[i]
    # left column for 4..7, right column for 0..3 (keeps wide reading order roughly)
    x = 16 + TW / 2 if side == 0 else 404 - TW / 2
    y = 70 + order * 126
    return x, y

def edge(cx, cy, tx, ty, r_tile=True):
    dx, dy = tx - cx, ty - cy
    d = math.hypot(dx, dy)
    x1, y1 = cx + dx / d * 50, cy + dy / d * 50
    # clip to tile rect
    sx = (TW / 2) / abs(dx) if dx else 1e9
    sy = (TH / 2) / abs(dy) if dy else 1e9
    s = min(sx, sy)
    x2, y2 = tx - dx * s - dx / d * 6, ty - dy * s - dy / d * 6
    return f"M{x1:.1f} {y1:.1f}L{x2:.1f} {y2:.1f}"

out = []
w = out.append
w(f'''<svg xmlns="http://www.w3.org/2000/svg" class="hr" viewBox="0 0 760 560" data-n-viewbox="0 0 420 560" role="img" aria-labelledby="hr-title hr-desc">
<title id="hr-title">One phone, one Brain, many agents on your computer</title>
<desc id="hr-desc">Your phone talks to one Brain on your own computer. Brain runs eight Workers on different agent clients: codex, claude, pi, grok and opencode. Some are working, some are done, one needs you.</desc>
<style>
.hr text{{white-space:pre;font-family:{SANS};fill:#171a18}}
.hr .m,.hr .m text{{font-family:{MONO}}}
.hr .mute{{fill:#7a817c}}.hr .ink2{{fill:#4a504c}}
.hr .tile rect.t{{fill:#fff;stroke:#e4e0d6;transition:stroke .2s}}
.hr .tile:hover rect.t{{stroke:#559e72}}
.hr .spoke{{stroke:#d9e8dd;stroke-width:1.5}}
.hr .st{{opacity:0}}
.hr .st-work{{opacity:1}}
.hr .static-you .st-work,.hr .static-done .st-work{{opacity:0}}
.hr .static-you .st-you,.hr .static-done .st-done{{opacity:1}}
@media (prefers-reduced-motion:no-preference){{
.hr .tile .st{{animation:hr-st 15s infinite both}}
.hr .tile .st-work{{animation-name:hr-work}}
.hr .tile .st-done{{animation-name:hr-done}}
.hr .tile .st-you{{animation-name:hr-you}}
.hr .live{{animation:hr-breathe 2.4s ease-in-out infinite;transform-origin:center;transform-box:fill-box}}
.hr .packet{{animation:hr-packet 3s ease-in-out infinite}}
.hr .halo{{animation:hr-halo 3.6s ease-out infinite;transform-origin:center;transform-box:fill-box}}
}}
@keyframes hr-work{{0%,55%{{opacity:1}}58%,100%{{opacity:0}}}}
@keyframes hr-done{{0%,57%{{opacity:0}}60%,97%{{opacity:1}}100%{{opacity:0}}}}
@keyframes hr-you{{0%,100%{{opacity:0}}}}
@keyframes hr-breathe{{0%,100%{{transform:scale(1)}}50%{{transform:scale(1.35)}}}}
@keyframes hr-packet{{0%{{stroke-dashoffset:0}}100%{{stroke-dashoffset:-160}}}}
@keyframes hr-halo{{0%{{opacity:.5;transform:scale(1)}}100%{{opacity:0;transform:scale(1.5)}}}}
''')
# per-tile animation offsets; the "needs you" tile keeps its clay state
for i in range(len(TILES)):
    w(f'.hr .tile-{i} .st{{animation-delay:-{(i*3.7) % 15:.1f}s}}')
w('.hr .tile-2 .st-you{animation-name:none;opacity:1}.hr .tile-2 .st-work,.hr .tile-2 .st-done{animation-name:none;opacity:0}')
w('</style>')
w('<rect class="hr-bg" width="100%" height="100%" rx="24" fill="#fbfaf7"/>')

# machine panel
w('<g class="hr-panel">')
w('<rect x="188.5" y="28.5" width="548" height="500" rx="26" fill="#f4f2ec" stroke="#e7e3d9" data-n-x="8.5" data-n-y="10.5" data-n-width="403" data-n-height="538"/>')
w('<text x="212" y="56" class="m mute" font-size="11.5" data-n-x="28" data-n-y="38">your computer · zen daemon</text>')
w('</g>')

# spokes
for i in range(len(TILES)):
    tx, ty = wide_pos(i)
    nx, ny = narrow_pos(i)
    w(f'<path class="spoke" d="{edge(BX, BY, tx, ty)}" data-n-d="{edge(NBX, NBY, nx, ny)}"/>')

# tiles
for i, (name, ex) in enumerate(TILES):
    tx, ty = wide_pos(i)
    nx, ny = narrow_pos(i)
    dx, dy = nx - tx, ny - ty
    cls = f"tile tile-{i}"
    if STATIC[i] != "work":
        cls += f" static-{STATIC[i]}"
    x, y = tx - TW / 2, ty - TH / 2
    w(f'<g class="{cls}" data-n-transform="translate({dx:.1f} {dy:.1f})">')
    w(f'<rect class="t" x="{x+.5:.1f}" y="{y+.5:.1f}" width="{TW}" height="{TH}" rx="14"/>')
    w(f'<text x="{x+16:.1f}" y="{y+22:.1f}" font-size="13" font-weight="600" class="m">{e(name)}</text>')
    w(f'<circle cx="{x+19:.1f}" cy="{y+36:.1f}" r="3.5" fill="{COL[ex]}"/>')
    w(f'<text x="{x+28:.1f}" y="{y+40:.1f}" font-size="11" class="m mute">{ex}</text>')
    sx, sy = x + TW - 18, y + TH / 2
    w(f'<g class="st st-work"><circle class="live" cx="{sx:.1f}" cy="{sy:.1f}" r="4.5" fill="#559e72"/></g>')
    w(f'<g class="st st-done"><circle cx="{sx:.1f}" cy="{sy:.1f}" r="7" fill="#2a5f41"/><path d="M{sx-3.2:.1f} {sy:.1f}l2.2 2.2 4.2-4.6" fill="none" stroke="#fff" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></g>')
    w(f'<g class="st st-you"><circle cx="{sx:.1f}" cy="{sy:.1f}" r="7" fill="#b47c6c"/><path d="M{sx:.1f} {sy-3.5:.1f}v3.6" stroke="#fff" stroke-width="1.8" stroke-linecap="round"/><circle cx="{sx:.1f}" cy="{sy+3.2:.1f}" r="1.1" fill="#fff"/></g>')
    w('</g>')

# Brain
w(f'<g class="hr-brain" data-n-transform="translate({NBX-BX} {NBY-BY})">')
w(f'<circle class="halo" cx="{BX}" cy="{BY}" r="46" fill="none" stroke="#559e72" stroke-width="1.5"/>')
w(f'<circle cx="{BX}" cy="{BY}" r="46" fill="#f1f9f3" stroke="#559e72" stroke-width="1.5"/>')
w(f'<g transform="translate({BX-29} {BY-36}) scale(.056)" fill="none" stroke="#2a5f41" stroke-linecap="round" stroke-linejoin="round">'
  '<path d="M790 372C724 252 590 209 461 244C334 279 252 393 266 524C282 675 411 778 558 760C650 749 731 697 781 620" stroke-width="64"/>'
  '<path d="M334 430C470 352 622 334 665 371C694 396 671 440 601 495C486 586 401 663 437 705C472 747 606 730 722 801" stroke-width="78"/></g>')
w(f'<text x="{BX}" y="{BY+27}" text-anchor="middle" font-size="12" font-weight="600" style="fill:#2a5f41">Brain</text>')
w('</g>')

# phone (wide only)
w('<g class="hr-phone" data-n-visibility="hidden">')
w('<path class="packet" d="M170 300C240 300 320 284 414 276" fill="none" stroke="#559e72" stroke-width="2" stroke-linecap="round" stroke-dasharray="2 10 2 146"/>')
w('<path d="M170 300C240 300 320 284 414 276" fill="none" stroke="#cfd9d1" stroke-width="1.5" stroke-dasharray="2 5"/>')
w('<rect x="22" y="132" width="176" height="356" rx="30" fill="#1f2421"/>')
w('<rect x="30" y="140" width="160" height="340" rx="23" fill="#fff"/>')
w('<rect x="88" y="147" width="44" height="12" rx="6" fill="#1f2421"/>')
w('<text x="110" y="186" text-anchor="middle" font-size="12" font-weight="700">Brain</text>')
w('<rect x="62" y="200" width="116" height="40" rx="13" fill="#dcf2e3"/>')
w('<text x="72" y="216" font-size="10.5">Finish the release.</text><text x="72" y="231" font-size="10.5">Ping me if stuck.</text>')
w('<circle cx="46" cy="257" r="3.5" fill="#559e72"/><text x="54" y="261" font-size="9.5" class="mute">Brain</text>')
w('<text x="42" y="280" font-size="10.5">8 Workers running.</text><text x="42" y="295" font-size="10.5">One question for you.</text>')
rows = [("ios-crash", "needs you", "#8f5a4b"), ("changelog", "done", "#2a5f41"), ("session-auth", "working", "#7a817c")]
for i, (n, s, c) in enumerate(rows):
    y = 314 + i * 38
    w(f'<rect x="40.5" y="{y+.5}" width="139" height="30" rx="9" fill="#fbfaf7" stroke="#ece9e1"/>')
    w(f'<text x="50" y="{y+19.5}" font-size="9.5" class="m">{n}</text><text x="171" y="{y+19.5}" font-size="9.5" text-anchor="end" style="fill:{c}">{s}</text>')
w('<rect x="40.5" y="440.5" width="139" height="26" rx="13" fill="#fff" stroke="#e4e0d6"/><text x="54" y="457" font-size="10" class="mute">Ask Brain</text>')
w('</g>')
w('</svg>\n')
sys.stdout.write("\n".join(out))
