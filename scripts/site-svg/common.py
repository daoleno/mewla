# Shared palette and drawing helpers for the README drawings.
# Every SVG must render on its own (README, GitHub camo): no webfonts, no
# script, one <style>. Colours and shapes follow app/DESIGN.md (Seal & Slip):
# paper and ink, vermilion only for the seal, Send and "Needs you", and one
# glyph per Work state.
import base64
import io
import os
import re
from html import escape

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..", "..")

SANS = 'Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif'
DISPLAY = '"Bricolage Grotesque",Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif'
MONO = '"Maple Mono","JetBrains Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace'

# AppColors, light scheme (app/theme/primitives.ts via DESIGN.md).
C = {
    "paper": "#FBFAF7",
    "card": "#FFFFFF",
    "tint": "#F5F3EE",
    "pressed": "#ECE8DF",
    "select": "#EFEBE3",
    "me": "#F0ECE4",
    "ink": "#161412",
    "soft": "#57514A",
    "soft2": "#6C665D",
    "line": "#ECE7DE",
    "border": "#DDD6C9",
    "faint": "#857E73",
    "seal": "#C8372B",
    "sealText": "#BC3328",
    "sealSoft": "#FBEBE6",
    "onSeal": "#FFFFFF",
    "ready": "#2F6B4F",
    "running": "#2C55C0",
    "warning": "#9A6212",
    "failed": "#7D1F35",
    "blocked": "#736C61",
    "desk": "#F1EEE7",
}


def e(s):
    return escape(str(s), quote=True)


def tw(s, size, mono=False, weight=400):
    """Rough text width, good enough to place trailing marks."""
    if mono:
        return len(s) * size * 0.6
    k = 0.53 if weight < 600 else 0.56
    return sum(size * (0.3 if ch in "il.,:;'|!" else 0.62 if ch.isupper() else k) for ch in s)


class Svg:
    def __init__(self, cls, w, h, title, desc, css=""):
        self.cls, self.w, self.h = cls, w, h
        self.title, self.desc, self.css = title, desc, css
        self.out = []

    def add(self, s):
        self.out.append(s)
        return self

    def text(self, x, y, s, size=14, fill="ink", weight=400, anchor="start", mono=False, display=False, cls="", extra=""):
        col = C.get(fill, fill)
        classes = " ".join(c for c in ("m" if mono else "", "d" if display else "", cls) if c)
        c = f' class="{classes}"' if classes else ""
        w = f' font-weight="{weight}"' if weight != 400 else ""
        a = f' text-anchor="{anchor}"' if anchor != "start" else ""
        self.out.append(f'<text x="{x:g}" y="{y:g}" font-size="{size:g}" fill="{col}"{w}{a}{c}{extra}>{e(s)}</text>')
        return self

    def rect(self, x, y, w, h, rx=0, fill="none", stroke=None, sw=1, cls="", extra=""):
        f = C.get(fill, fill)
        s = f' stroke="{C.get(stroke, stroke)}" stroke-width="{sw:g}"' if stroke else ""
        c = f' class="{cls}"' if cls else ""
        r = f' rx="{rx:g}"' if rx else ""
        self.out.append(f'<rect x="{x:g}" y="{y:g}" width="{w:g}" height="{h:g}"{r} fill="{f}"{s}{c}{extra}/>')
        return self

    def line(self, x1, y1, x2, y2, stroke="line", sw=1, cls="", extra=""):
        c = f' class="{cls}"' if cls else ""
        self.out.append(f'<path d="M{x1:g} {y1:g}L{x2:g} {y2:g}" stroke="{C.get(stroke, stroke)}" stroke-width="{sw:g}"{c}{extra}/>')
        return self

    def circle(self, cx, cy, r, fill="none", stroke=None, sw=1, cls="", extra=""):
        f = C.get(fill, fill)
        s = f' stroke="{C.get(stroke, stroke)}" stroke-width="{sw:g}"' if stroke else ""
        c = f' class="{cls}"' if cls else ""
        self.out.append(f'<circle cx="{cx:g}" cy="{cy:g}" r="{r:g}" fill="{f}"{s}{c}{extra}/>')
        return self

    def path(self, d, stroke="line", sw=1, fill="none", cls="", extra=""):
        c = f' class="{cls}"' if cls else ""
        s = f' stroke="{C.get(stroke, stroke)}" stroke-width="{sw:g}"' if stroke else ""
        self.out.append(f'<path d="{d}" fill="{C.get(fill, fill)}"{s}{c}{extra}/>')
        return self

    def g(self, cls="", extra=""):
        c = f' class="{cls}"' if cls else ""
        self.out.append(f"<g{c}{extra}>")
        return self

    def end(self):
        self.out.append("</g>")
        return self

    def render(self, bg="desk", rx=20):
        k = self.cls
        head = (
            f'<svg xmlns="http://www.w3.org/2000/svg" class="{k}" viewBox="0 0 {self.w} {self.h}" '
            f'width="{self.w}" height="{self.h}" role="img" aria-labelledby="{k}-t {k}-d" fill="none">'
            f'<title id="{k}-t">{e(self.title)}</title><desc id="{k}-d">{e(self.desc)}</desc>'
            f"<style>.{k} text{{font-family:{SANS};white-space:pre}}.{k} .m{{font-family:{MONO}}}"
            f".{k} .d{{font-family:{DISPLAY};letter-spacing:-.02em}}"
            f".{k} .spin{{transform-box:fill-box;transform-origin:center;animation:{k}-spin 1.1s linear infinite}}"
            f"@keyframes {k}-spin{{to{{transform:rotate(360deg)}}}}"
            f"@media (prefers-reduced-motion:reduce){{.{k} .spin{{animation:none}}}}{self.css}</style>"
        )
        body = []
        if bg:
            body.append(f'<rect width="{self.w}" height="{self.h}" rx="{rx}" fill="{C[bg]}"/>')
        return head + "".join(body) + "".join(self.out) + "</svg>\n"


# --- The mark and the pet ---------------------------------------------------
# Raster art (generated, see docs/third-party-assets.md), embedded at 2x.

def _png_uri(path, width, frame=0):
    from PIL import Image  # noqa: PLC0415

    with Image.open(path) as image:
        image.seek(frame)
        image.load()
        still = image.convert("RGBA")
    still = still.resize((round(width * 2), round(width * 2 * still.height / still.width)), Image.Resampling.LANCZOS)
    buffer = io.BytesIO()
    still.save(buffer, format="PNG", optimize=True)
    return "data:image/png;base64," + base64.b64encode(buffer.getvalue()).decode()


def seal(s, x, y, size):
    """The brand mark (site/mark.png), size px square."""
    uri = _png_uri(os.path.join(ROOT, "site", "mark.png"), size)
    s.add(f'<image x="{x:g}" y="{y:g}" width="{size:g}" height="{size:g}" href="{uri}"/>')


# The default pet's canvas: square, feet on this line of 256.
PET_GROUND = 232 / 256


def pet(s, clip, x, y, width, frame=-1):
    """The default pet in one frame of a clip, feet on y, centred on x."""
    path = os.path.join(ROOT, "app", "assets", "pets", "p05", f"{clip}.webp")
    if frame < 0:
        from PIL import Image  # noqa: PLC0415

        with Image.open(path) as image:
            frame = getattr(image, "n_frames", 1) + frame
    uri = _png_uri(path, width, frame)
    top = y - width * PET_GROUND
    s.add(f'<image x="{x - width / 2:g}" y="{top:g}" width="{width:g}" height="{width:g}" href="{uri}"/>')


# --- Status marks (components/ui/StatusMark.tsx) ---------------------------

STATE_WORD = {
    "ready": "Ready", "running": "Working", "needs": "Needs you", "warning": "Needs review",
    "failed": "Failed", "blocked": "Blocked", "waiting": "Waiting",
}


def mark(s, kind, cx, cy, r=6.5, spin=True):
    """One 13 pt glyph per state, centred on (cx, cy)."""
    if kind == "ready":
        s.circle(cx, cy, r, fill="ready")
        s.path(f"M{cx - r * .45:g} {cy + r * .02:g}l{r * .32:g} {r * .32:g}l{r * .6:g} {-r * .62:g}", stroke="#FFFFFF", sw=1.6,
               extra=' stroke-linecap="round" stroke-linejoin="round"')
    elif kind == "running":
        a = 2 * 3.14159 * (r - 1)
        s.circle(cx, cy, r - 1, stroke="running", sw=1.8, cls="spin" if spin else "",
                 extra=f' stroke-dasharray="{a * .7:.2f} {a:.2f}" stroke-linecap="round"')
    elif kind == "warning":
        s.path(f"M{cx:g} {cy - r:g}L{cx + r:g} {cy + r * .8:g}H{cx - r:g}Z", stroke="warning", sw=1.5, extra=' stroke-linejoin="round"')
        s.line(cx, cy - r * .25, cx, cy + r * .2, stroke="warning", sw=1.5)
        s.circle(cx, cy + r * .5, .8, fill="warning")
    elif kind == "failed":
        s.rect(cx - r + .8, cy - r + .8, 2 * r - 1.6, 2 * r - 1.6, rx=2, stroke="failed", sw=1.5)
        s.path(f"M{cx - r * .4:g} {cy - r * .4:g}L{cx + r * .4:g} {cy + r * .4:g}M{cx + r * .4:g} {cy - r * .4:g}L{cx - r * .4:g} {cy + r * .4:g}",
               stroke="failed", sw=1.5, extra=' stroke-linecap="round"')
    elif kind in ("blocked", "waiting"):
        s.circle(cx, cy, r - 1, stroke="blocked", sw=1.5, extra=' stroke-dasharray="2 2.2"')


def state(s, kind, x, y, anchor="end", size=13, word=None, spin=True):
    """A state mark plus its word on baseline y. anchor=end puts it flush right at x."""
    word = STATE_WORD[kind] if word is None else word
    if kind == "needs":
        w = tw(word, size - 1, weight=600) + 18
        x0 = x - w if anchor == "end" else x
        s.rect(x0, y - size + 1, w, size + 7, rx=(size + 7) / 2, fill="seal")
        s.text(x0 + w / 2, y + 1, word, size=size - 1, fill="onSeal", weight=600, anchor="middle")
        return w
    col = {"ready": "ready", "running": "running", "warning": "warning", "failed": "failed", "blocked": "soft", "waiting": "soft"}[kind]
    w = tw(word, size, weight=500) + 19
    x0 = x - w if anchor == "end" else x
    mark(s, kind, x0 + 6.5, y - size * .36, spin=spin)
    s.text(x0 + 19, y, word, size=size, fill=col, weight=500)
    return w


# --- Pieces of the app -----------------------------------------------------

def slip(s, x, y, w, meta, title, line=None, kind="ready", h=None, perch=False, dot=False):
    """A Work slip (WorkSlip): meta, state, title, one line."""
    h = h or (96 if line else 72)
    if kind == "needs":
        s.rect(x, y, w, h, rx=14, fill="card", stroke="ink", sw=1.5)
    elif kind in ("blocked", "waiting"):
        s.rect(x, y, w, h, rx=14, fill="paper", stroke="border", extra=' stroke-dasharray="4 4"')
    else:
        s.rect(x, y, w, h, rx=14, fill="card", stroke="line")
    s.text(x + 18, y + 27, meta, size=12.5, fill="soft")
    sw_ = state(s, kind, x + w - 16, y + 28, size=12.5)
    if dot:
        s.circle(x + w - 16 - sw_ - 9, y + 23.5, 3, fill="ink")
    s.text(x + 18, y + 55, title, size=16, weight=600)
    if line:
        s.text(x + 18, y + 80, line, size=14, fill="soft")
    if perch:
        pet(s, "attention", x + w - 50, y + 6, 84)
    return h


def chevron(s, x, y, size=7, stroke="soft", direction="right"):
    d = {"right": f"M{x:g} {y - size:g}l{size:g} {size:g}l{-size:g} {size:g}",
         "left": f"M{x + size:g} {y - size:g}l{-size:g} {size:g}l{size:g} {size:g}",
         "down": f"M{x - size:g} {y - size / 2:g}l{size:g} {size:g}l{size:g} {-size:g}"}[direction]
    s.path(d, stroke=stroke, sw=1.6, extra=' stroke-linecap="round" stroke-linejoin="round"')


def round_button(s, cx, cy, r=18, fill="card", stroke="line"):
    s.circle(cx, cy, r, fill=fill, stroke=stroke)


def menu_icon(s, cx, cy):
    s.path(f"M{cx - 7:g} {cy - 3:g}H{cx + 7:g}M{cx - 7:g} {cy + 3:g}H{cx + 7:g}", stroke="ink", sw=1.7, extra=' stroke-linecap="round"')


def dots_icon(s, cx, cy, col="ink"):
    for i in (-6, 0, 6):
        s.circle(cx + i, cy, 1.7, fill=col)


def screen_header(s, x, y, w, title, back=True, trailing=None):
    """A pushed screen's header: back chevron, centred title."""
    if back:
        round_button(s, x + 26, y + 26, 18)
        chevron(s, x + 23, y + 26, 6, stroke="ink", direction="left")
    s.text(x + w / 2, y + 32, title, size=18, weight=600, anchor="middle")
    if trailing == "dots":
        round_button(s, x + w - 26, y + 26, 18)
        dots_icon(s, x + w - 26, y + 26)


def segmented(s, x, y, w, labels, on=0, h=40):
    s.rect(x, y, w, h, rx=h / 2, fill="tint")
    seg = w / len(labels)
    s.rect(x + on * seg + 4, y + 4, seg - 8, h - 8, rx=(h - 8) / 2, fill="card", stroke="line")
    for i, lab in enumerate(labels):
        s.text(x + i * seg + seg / 2, y + h / 2 + 5, lab, size=14, fill="ink" if i == on else "soft", weight=500 if i == on else 400, anchor="middle")


def _logos():
    """Product marks from the homepage sprite (LobeHub Icons, MIT; Simple Icons, CC0)."""
    with open(os.path.join(ROOT, "site", "index.html")) as f:
        src = f.read()
    return {m.group(1): (m.group(2), m.group(3), m.group(4)) for m in
            re.finditer(r'<symbol id="lg-([a-z]+)" viewBox="([^"]+)"([^>]*)>(.*?)</symbol>', src, re.S)}


LOGOS = _logos()


def logo(s, name, x, y, size, color="ink"):
    vb, attrs, inner = LOGOS[name]
    s.add(f'<svg x="{x:g}" y="{y:g}" width="{size:g}" height="{size:g}" viewBox="{vb}" color="{C.get(color, color)}">'
          f'<g{attrs if "fill=" in attrs else ' fill="currentColor"' + attrs}>{inner}</g></svg>')


def client_badge(s, cx, cy, client, r=15):
    """Agent client avatar: its product mark on a quiet tile; a shell gets a prompt."""
    s.rect(cx - r, cy - r, 2 * r, 2 * r, rx=r * .45, fill="card", stroke="line")
    if client in LOGOS:
        logo(s, client, cx - r * .62, cy - r * .62, r * 1.24)
    else:
        s.text(cx, cy + r * .3, "$", size=r * .9, fill="ink", weight=600, anchor="middle", mono=True)


def phone(s, x, y, w, h):
    """Thin device outline on paper. Returns the screen rect (x, y, w, h)."""
    s.rect(x, y, w, h, rx=44, fill="card", stroke="border", sw=1.5)
    i = 8
    s.rect(x + i, y + i, w - 2 * i, h - 2 * i, rx=36, fill="paper")
    s.rect(x + w / 2 - 44, y + 18, 88, 22, rx=11, fill="ink")
    return x + i, y + i, w - 2 * i, h - 2 * i


def write(path, data):
    with open(path, "w") as f:
        f.write(data)
