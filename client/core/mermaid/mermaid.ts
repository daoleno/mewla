/**
 * Mermaid diagrams in Markdown (owner 11-16: 「flowchart 在 mewla 里也要支持一下」): a fenced block tagged `mermaid`
 * in a chat reply or a previewed file is drawn as its diagram. The drawing is Pele's (`pele.js`, vendored by
 * `vendor-pele.mjs`; owner 11-16: 「mermaid 可以用这个 https://pele.run/」): Mermaid syntax to an SVG string, with
 * no DOM, in the app itself and offline. What it draws is shown in a web view that runs no script
 * (`mermaidSvgDocument`), coloured by CSS variables, so light and dark are one drawing.
 *
 * - Pele lays a wide diagram out again for the width it is given (`maxWidth`: a `flowchart LR` may turn
 *   vertical), and what is still wider is shown at a scale that keeps its labels legible and scrolls sideways
 *   (`mermaidInlineLayout`; owner 11-16, a-light-top.png: fitted, labels were some 4 px).
 * - A diagram Pele cannot read keeps Pele's own words (`error`, Mermaid's: "Parse error on line 3: …"), shown
 *   under its code.
 * - Each (source, width) is drawn once and kept (MERMAID_CACHE_LIMIT), errors too.
 */

import { render } from "./pele.js";

export type MermaidMode = "light" | "dark";

export type MermaidDrawing = { ok: true; svg: string; width: number; height: number; type: string };
export type MermaidResult = MermaidDrawing | { ok: false; error: string };

export type MermaidPrepared = { ok: true; source: string } | { ok: false; error: string };

export const MERMAID_MAX_SOURCE = 32_768;
export const MERMAID_MAX_SVG = 2 << 20;
/** The size labels are laid out at. */
export const MERMAID_FONT_PX = 14;
/** The smallest a label is shown in a reply or a file: below it a diagram scrolls sideways instead of shrinking. */
export const MERMAID_MIN_LABEL_PX = 11;
export const MERMAID_CACHE_LIMIT = 64;
/** The font the layout measures and the view draws in (CJK included). */
export const MERMAID_FONT_FAMILY = "system-ui, -apple-system, 'Segoe UI', Roboto, 'Noto Sans CJK SC', 'PingFang SC', sans-serif";

/** Whether a fenced block's info string names mermaid (`mermaid`, `Mermaid title="x"`). */
export function isMermaidLanguage(language: string | undefined): boolean {
  return /^mermaid$/i.test((language ?? "").trim().split(/\s+/)[0] ?? "");
}

/** The source as it is drawn, or why it is not. Front matter, `%%{init}%%`, `click` and labels are Pele's to read. */
export function prepareMermaid(raw: string): MermaidPrepared {
  const source = raw.replace(/\r\n?/g, "\n").trim();
  if (!source) return { ok: false, error: "The diagram is empty." };
  if (source.length > MERMAID_MAX_SOURCE) return { ok: false, error: `The diagram is over ${MERMAID_MAX_SOURCE / 1024} KB, too large to draw.` };
  return { ok: true, source };
}

/** What is shown, besides Pele's own escaping: no script, no handler, no script URL (its view runs none anyway). */
export function sanitizeMermaidSvg(svg: string): string {
  return svg
    .replace(/<script\b[\s\S]*?<\/script\s*>/gi, "")
    .replace(/<foreignObject\b[\s\S]*?<\/foreignObject\s*>/gi, "")
    .replace(/\son[a-z]+\s*=\s*(?:"[^"]*"|'[^']*')/gi, "")
    .replace(/\s(?:href|xlink:href)\s*=\s*(["'])\s*(?:javascript|vbscript|data):[\s\S]*?\1/gi, "");
}

/** Who wants to know of each drawing made (tests: that a streaming reply is not drawn, that a theme draws nothing). */
let renderSink: ((entry: { source: string; maxWidth: number | null }) => void) | null = null;
export function setMermaidRenderSink(sink: typeof renderSink): void {
  renderSink = sink;
}

const cache = new Map<string, MermaidResult>();

/**
 * The diagram drawn to fit `maxWidth` (laid out again where Pele can), or as written (`null`: the full-screen view),
 * made once per (source, width) and kept.
 */
export function drawMermaid(source: string, maxWidth: number | null): MermaidResult {
  const key = `${maxWidth ?? "-"}\n${source}`;
  const kept = cache.get(key);
  if (kept) {
    cache.delete(key);
    cache.set(key, kept);
    return kept;
  }
  renderSink?.({ source, maxWidth });
  let result: MermaidResult;
  try {
    const drawn = render(source, {
      fontFamily: MERMAID_FONT_FAMILY,
      fontSize: MERMAID_FONT_PX,
      responsive: false,
      links: false,
      images: false,
      outputLimit: MERMAID_MAX_SVG,
      idPrefix: `m${cache.size.toString(36)}`,
      ...(maxWidth === null ? { autoDirection: false } : { maxWidth: Math.max(160, Math.round(maxWidth)), autoDirection: true }),
    });
    result = drawn.svg && drawn.width > 0 && drawn.height > 0
      ? { ok: true, svg: sanitizeMermaidSvg(drawn.svg), width: drawn.width, height: drawn.height, type: drawn.type }
      : { ok: false, error: "The diagram came out empty." };
  } catch (error) {
    const words = error instanceof Error ? error.message : String(error);
    result = { ok: false, error: (words.trim() || "The diagram could not be drawn.").slice(0, 1200) };
  }
  cache.set(key, result);
  while (cache.size > MERMAID_CACHE_LIMIT) cache.delete(cache.keys().next().value as string);
  return result;
}

/** How a drawn diagram is shown in a reply or a file, `room` wide and at most `maxHeight` tall. */
export type MermaidInlineLayout = {
  /** Of its drawn size: never above 1, never below what keeps labels MERMAID_MIN_LABEL_PX. */
  scale: number;
  /** Its size at that scale. */
  width: number;
  height: number;
  /** The frame's height: the diagram's, or `maxHeight` when it is taller (its top shown, the rest full screen). */
  frameHeight: number;
  /** Wider than `room` at that scale: its frame scrolls sideways. */
  scrolls: boolean;
  /** Taller than `maxHeight`: cut at the frame's foot. */
  cropped: boolean;
};

/**
 * Owner 11-16 (a-light-top.png): fitted to a phone's width, a wide `flowchart LR` shrank its labels to some 4 px.
 * It is fitted only while its labels stay MERMAID_MIN_LABEL_PX; a wider one is shown at that scale and scrolls
 * sideways, a taller one is cut at `maxHeight`; either opens whole full screen.
 */
export function mermaidInlineLayout(size: { width: number; height: number }, room: number, maxHeight: number): MermaidInlineLayout {
  const legible = MERMAID_MIN_LABEL_PX / MERMAID_FONT_PX;
  const fit = room > 0 ? room / size.width : 1;
  const scale = Math.min(1, Math.max(fit, legible));
  const width = Math.ceil(size.width * scale);
  const height = Math.ceil(size.height * scale);
  return { scale, width, height, frameHeight: Math.min(height, maxHeight), scrolls: room > 0 && width > room + 0.5, cropped: height > maxHeight };
}

/** The colours a diagram is shown in: the app's own, light or dark. */
export type MermaidPalette = {
  surface: string;
  ink: string;
  soft: string;
  node: string;
  border: string;
  cluster: string;
};

const COLOR = /^#[0-9a-f]{3,8}$/i;

/** Pele's CSS variables in the app's colours (pele.run/theming). */
export function mermaidCssVariables(palette: MermaidPalette): string {
  const color = (value: string) => (COLOR.test(value) ? value : "#808080");
  return [
    `--pele-bg:${color(palette.surface)}`,
    `--pele-fg:${color(palette.ink)}`,
    `--pele-muted:${color(palette.soft)}`,
    `--pele-line:${color(palette.soft)}`,
    `--pele-surface:${color(palette.node)}`,
    `--pele-surface-alt:${color(palette.cluster)}`,
    `--pele-border:${color(palette.border)}`,
    `--pele-accent:${color(palette.ink)}`,
    `--pele-font:${MERMAID_FONT_FAMILY}`,
    "--pele-radius:6px",
  ].join(";");
}

const VIEW_CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; script-src 'none'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";

/**
 * A drawn diagram as a page that runs no script, in `palette`. Fitted (`scale` null): the whole diagram inside the
 * view, no larger than drawn. Else at `scale` times its drawn size, scrolled to pan (`pad` around it); `inline`: at
 * that size in a view its own size (a reply's frame), its top left, cut below.
 */
export function mermaidSvgDocument(drawing: { svg: string; width: number; height: number }, palette: MermaidPalette, scale: number | null = null, inline = false): string {
  const width = scale === null ? 0 : Math.ceil(drawing.width * scale);
  const height = scale === null ? 0 : Math.ceil(drawing.height * scale);
  const sized = scale === null
    ? `html,body{height:100%;overflow:hidden}body{display:flex;align-items:center;justify-content:center}svg{display:block;width:100%!important;height:100%!important;max-width:${drawing.width}px!important;max-height:${drawing.height}px!important}`
    : inline
      ? `html,body{overflow:hidden}svg{display:block;width:${width}px!important;height:${height}px!important;max-width:none!important}`
      : `html,body{overflow:auto}body{padding:16px;box-sizing:border-box;width:max-content}svg{display:block;width:${width}px!important;height:${height}px!important;max-width:none!important}`;
  const background = COLOR.test(palette.surface) ? palette.surface : "transparent";
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${VIEW_CSP}"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;background:${background}}body{${mermaidCssVariables(palette)}}${sized}</style></head><body>${drawing.svg}</body></html>`;
}
