// Styled history rows for the browser's terminal page (client/ui/web/terminal/terminal.js):
// the SGR styling of a row as runs of text with CSS colors, so the page only
// builds elements. Palette colors 0-15 stay CSS variables (`--c0`..`--c15`),
// which the page sets from the theme, so a light/dark switch recolors rows
// already drawn; the 256-color cube and truecolor are resolved here.

/** A run of one style: `t` its text, `c` / `g` its CSS text and background colors (none: the theme's), `f` its classes. */
export type WebSpan = { t: string; c?: string; g?: string; f?: string };

type Color = number | string | null;

type Pen = {
  fg: Color;
  bg: Color;
  bold: boolean;
  dim: boolean;
  italic: boolean;
  underline: boolean;
  inverse: boolean;
  invisible: boolean;
  strike: boolean;
  overline: boolean;
};

const plain = (): Pen => ({ fg: null, bg: null, bold: false, dim: false, italic: false, underline: false, inverse: false, invisible: false, strike: false, overline: false });

const CUBE = [0, 95, 135, 175, 215, 255];
const hex2 = (n: number) => Math.max(0, Math.min(255, n | 0)).toString(16).padStart(2, "0");

/** xterm's 256 colors: 0-15 the theme's (CSS variables), then the 6×6×6 cube and 24 grays. */
export function color256(index: number): string {
  if (index < 16) return `var(--c${index})`;
  if (index < 232) {
    const i = index - 16;
    return `#${hex2(CUBE[Math.floor(i / 36)])}${hex2(CUBE[Math.floor(i / 6) % 6])}${hex2(CUBE[i % 6])}`;
  }
  const v = 8 + (index - 232) * 10;
  return `#${hex2(v)}${hex2(v)}${hex2(v)}`;
}

function paletteIndex(value: string | undefined): number | undefined {
  const n = Number(value);
  return value !== undefined && value !== "" && Number.isInteger(n) && n >= 0 && n <= 255 ? n : undefined;
}

function rgb(parts: (string | undefined)[]): string | undefined {
  if (parts.length < 3) return undefined;
  const values = parts.slice(0, 3).map((part) => Number(part === "" ? 0 : part));
  return values.every((v) => Number.isInteger(v) && v >= 0 && v <= 255) ? `#${values.map(hex2).join("")}` : undefined;
}

/** One SGR sequence's parameters (`1;38;5;196`, `38:2::255:0:0`) applied to the pen. */
function applySgr(pen: Pen, params: string): void {
  const parts = params === "" ? ["0"] : params.split(";");
  for (let i = 0; i < parts.length; i++) {
    const sub = parts[i].split(":");
    const code = sub[0] === "" ? 0 : Number(sub[0]);
    if (code === 38 || code === 48 || code === 58) {
      let color: Color | undefined;
      if (sub.length > 1) {
        // Colon form: 38:5:n, 38:2:r:g:b or 38:2:<colorspace>:r:g:b.
        if (sub[1] === "5") color = paletteIndex(sub[2]);
        else if (sub[1] === "2") color = rgb(sub.length >= 6 ? sub.slice(3, 6) : sub.slice(2, 5));
      } else if (parts[i + 1] === "5") {
        color = paletteIndex(parts[i + 2]);
        i += 2;
      } else if (parts[i + 1] === "2") {
        color = rgb(parts.slice(i + 2, i + 5));
        i += 4;
      }
      if (color === undefined) continue;
      if (code === 38) pen.fg = color;
      else if (code === 48) pen.bg = color;
      // 58 (the underline's color) has no counterpart here.
      continue;
    }
    switch (code) {
      case 0: Object.assign(pen, plain()); break;
      case 1: pen.bold = true; break;
      case 2: pen.dim = true; break;
      case 3: pen.italic = true; break;
      case 4: pen.underline = sub[1] !== "0"; break;
      case 7: pen.inverse = true; break;
      case 8: pen.invisible = true; break;
      case 9: pen.strike = true; break;
      case 21: pen.underline = true; break;
      case 22: pen.bold = false; pen.dim = false; break;
      case 23: pen.italic = false; break;
      case 24: pen.underline = false; break;
      case 27: pen.inverse = false; break;
      case 28: pen.invisible = false; break;
      case 29: pen.strike = false; break;
      case 39: pen.fg = null; break;
      case 49: pen.bg = null; break;
      case 53: pen.overline = true; break;
      case 55: pen.overline = false; break;
      default:
        if (code >= 30 && code <= 37) pen.fg = code - 30;
        else if (code >= 40 && code <= 47) pen.bg = code - 40;
        else if (code >= 90 && code <= 97) pen.fg = code - 90 + 8;
        else if (code >= 100 && code <= 107) pen.bg = code - 100 + 8;
    }
  }
}

const css = (color: Color): string | undefined => (color === null ? undefined : typeof color === "number" ? color256(color) : color);

/** The pen as a span's colors and classes: bold draws palette 0-7 bright (xterm's default), inverse swaps, invisible hides. */
function style(pen: Pen): Omit<WebSpan, "t"> {
  const fgColor = pen.bold && typeof pen.fg === "number" && pen.fg < 8 ? pen.fg + 8 : pen.fg;
  let c = css(fgColor);
  let g = css(pen.bg);
  if (pen.inverse) [c, g] = [g ?? "var(--bg)", c ?? "var(--fg)"];
  if (pen.invisible) c = "transparent";
  const f = [pen.bold && "B", pen.dim && "D", pen.italic && "I", pen.underline && "U", pen.strike && "S", pen.overline && "O"].filter(Boolean).join(" ");
  return { ...(c ? { c } : {}), ...(g ? { g } : {}), ...(f ? { f } : {}) };
}

// CSI (its parameters, intermediates and final byte), OSC (BEL or ST), the other string
// sequences and two-byte escapes; C0 controls but tab.
const ESCAPES = /\u001b\[([0-?]*)([ -/]*)([@-~])|\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)?|\u001b[P^_X][^\u001b]*(?:\u001b\\)?|\u001b[ -/]*[0-~]|[\u0000-\u0008\u000a-\u001f\u007f]/g;

/** A styled history row (ANSI) as runs of one style each, adjacent runs of the same style merged. */
export function ansiSpans(ansi: string): WebSpan[] {
  const spans: WebSpan[] = [];
  const pen = plain();
  let current = style(pen);
  const push = (text: string) => {
    if (!text) return;
    const last = spans[spans.length - 1];
    if (last && last.c === current.c && last.g === current.g && last.f === current.f) last.t += text;
    else spans.push({ t: text, ...current });
  };
  let at = 0;
  ESCAPES.lastIndex = 0;
  for (let match = ESCAPES.exec(ansi); match; match = ESCAPES.exec(ansi)) {
    push(ansi.slice(at, match.index));
    at = match.index + match[0].length;
    const [, params, intermediates, final] = match;
    // SGR only: private parameter markers (`<`, `=`, `>`, `?`) and intermediates are other sequences.
    if (final === "m" && !intermediates && params !== undefined && !/^[<=>?]/.test(params)) {
      applySgr(pen, params);
      current = style(pen);
    }
  }
  push(ansi.slice(at));
  return spans;
}
