# Shared palette and drawing helpers for the homepage and README drawings.
# Every SVG must render on its own (README, GitHub camo): no webfonts, no
# script, default state in its own <style>. The page inlines the same files
# and drives data-state, so every rule is scoped to the figure's root class.
from html import escape

SANS = 'Archivo,ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif'
MONO = '"Geist Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace'

# Matches the app's dark tokens: warm ink canvas, ivory brand detail, sage
# accent, teal success. Ember marks the one thing that needs the owner.
C = {
    "bg": "#0c0d0a",
    "panel": "#131410",
    "panel2": "#1a1b16",
    "panel3": "#22231d",
    "line": "#2a2b24",
    "line2": "#3b3c34",
    "ink": "#f2eee5",
    "dim": "#aaa699",
    "faint": "#77746a",
    "sage": "#8fcfa6",
    "sage2": "#5f9d78",
    "sageD": "#1f3328",
    "teal": "#5ec4b6",
    "ember": "#f0895c",
    "emberD": "#3a2219",
    "amber": "#e2b45c",
    "red": "#ef6b5f",
}


def e(s):
    return escape(str(s), quote=True)


class Svg:
    def __init__(self, cls, w, h, title, desc, states=0, css=""):
        self.cls, self.w, self.h = cls, w, h
        self.title, self.desc, self.states, self.css = title, desc, states, css
        self.out = []

    def add(self, s):
        self.out.append(s)
        return self

    def text(self, x, y, s, size=13, fill="ink", weight=400, anchor="start", mono=False, cls="", extra=""):
        col = C.get(fill, fill)
        fam = ' class="m' + (" " + cls if cls else "") + '"' if mono else (f' class="{cls}"' if cls else "")
        w = f' font-weight="{weight}"' if weight != 400 else ""
        a = f' text-anchor="{anchor}"' if anchor != "start" else ""
        self.out.append(f'<text x="{x:g}" y="{y:g}" font-size="{size:g}" fill="{col}"{w}{a}{fam}{extra}>{e(s)}</text>')
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

    def render(self, bg=True, rx=18, live_vb=None):
        k = self.cls
        state_css = ""
        if self.states:
            state_css += f".{k} .st{{opacity:0;transition:opacity .4s ease}}"
            on = [f".{k}:not([data-state]) .s1"] + [f'.{k}[data-state="{i}"] .s{i}' for i in range(1, self.states + 1)]
            state_css += ",".join(on) + "{opacity:1}"
            state_css += f"@media (prefers-reduced-motion:reduce){{.{k} .st{{transition:none}}}}"
        head = (
            f'<svg xmlns="http://www.w3.org/2000/svg" class="{k}" viewBox="0 0 {self.w} {self.h}" '
            f'width="{self.w}" height="{self.h}" role="img" aria-labelledby="{k}-t {k}-d" fill="none">'
            f'<title id="{k}-t">{e(self.title)}</title><desc id="{k}-d">{e(self.desc)}</desc>'
            f"<style>.{k} text{{font-family:{SANS};white-space:pre}}.{k} .m{{font-family:{MONO}}}"
            f"{state_css}{self.css}</style>"
        )
        body = []
        if bg:
            body.append(f'<rect class="bg" width="{self.w}" height="{self.h}" rx="{rx}" fill="{C["bg"]}"/>')
        return head + "".join(body) + "".join(self.out) + "</svg>\n"


def ticks(s, x, y, w, h, n=8, col="line"):
    """Registration corners: the drawing's only ornament."""
    L = 10
    for cx, cy, dx, dy in ((x, y, 1, 1), (x + w, y, -1, 1), (x, y + h, 1, -1), (x + w, y + h, -1, -1)):
        s.path(f"M{cx:g} {cy + dy * L:g}V{cy:g}H{cx + dx * L:g}", stroke=col, sw=1)


def phone(s, x, y, w, h, screen="panel"):
    """Thin device outline. Returns the screen rect (x, y, w, h)."""
    s.rect(x, y, w, h, rx=w * 0.15, fill="#0a0b08", stroke="line2", sw=1.2)
    i = w * 0.032
    sx, sy, sw_, sh = x + i, y + i, w - 2 * i, h - 2 * i
    s.rect(sx, sy, sw_, sh, rx=w * 0.125, fill=screen)
    s.rect(x + w / 2 - w * 0.13, sy + 9, w * 0.26, 18, rx=9, fill="#050504")
    return sx, sy, sw_, sh


def pill(s, x, y, label, fill="panel3", col="dim", size=11, pad=8, h=20, mono=False, stroke=None, anchor="start"):
    w = len(label) * size * (0.62 if mono else 0.56) + pad * 2
    if anchor == "end":
        x -= w
    s.rect(x, y, w, h, rx=h / 2, fill=fill, stroke=stroke)
    s.text(x + w / 2, y + h / 2 + size * 0.36, label, size=size, fill=col, anchor="middle", mono=mono, weight=500)
    return w


def dot(s, x, y, kind, r=3.5, cls=""):
    col = {"run": "sage", "need": "ember", "done": "teal", "idle": "faint", "fail": "red"}[kind]
    s.circle(x, y, r, fill=col, cls=cls)


def write(path, data):
    with open(path, "w") as f:
        f.write(data)
