/**
 * Mermaid diagrams in Markdown (owner 11-16: 「flowchart 在 mewla 里也要支持一下」): a fenced block tagged `mermaid`
 * in a chat reply or a previewed file is drawn as its diagram. The drawing is mermaid's own, run offline in one
 * engine web view the app holds (`ui/mermaid/engine.tsx`); what it draws is an SVG, shown in a web view that runs
 * no script (`mermaidSvgDocument`). This is the part with no view: what is drawn, one drawing at a time, kept.
 *
 * - The source is drawn as written, minus what reaches outside it: `%%{init}%%` and front matter (a config of
 *   its own), `click` lines (links and callbacks), and HTML tags other than `<br>` in labels.
 * - A diagram mermaid cannot read keeps mermaid's own words (`error`), shown under its code.
 * - Each (source, theme) is drawn once; the answer is kept (MERMAID_CACHE_LIMIT), errors too, so a row drawn
 *   again (a list cell made anew, a streaming reply's next words elsewhere) shows it at once.
 */

export type MermaidMode = "light" | "dark";

export type MermaidResult =
  | { ok: true; svg: string; width: number; height: number }
  | { ok: false; error: string };

export type MermaidPrepared = { ok: true; source: string } | { ok: false; error: string };

export const MERMAID_MAX_SOURCE = 32_768;
export const MERMAID_MAX_SVG = 2 << 20;
export const MERMAID_CACHE_LIMIT = 48;
/** How long one drawing may take once the engine has it. */
export const MERMAID_RENDER_TIMEOUT_MS = 15_000;
/** How long the engine may take to load (5 MB of script on a slow phone) before every waiting drawing fails. */
export const MERMAID_ENGINE_TIMEOUT_MS = 30_000;

const FRONTMATTER = /^---\r?\n[\s\S]*?\r?\n---\r?\n/;
const DIRECTIVE = /%%\{[\s\S]*?\}%%/g;
const CLICK_LINE = /^\s*click\s+\S+.*$/gim;
const HTML_TAG = /<\/?([a-zA-Z][\w:-]*)\b[^>]*>/g;

/** Whether a fenced block's info string names mermaid (`mermaid`, `Mermaid title="x"`). */
export function isMermaidLanguage(language: string | undefined): boolean {
  return /^mermaid$/i.test((language ?? "").trim().split(/\s+/)[0] ?? "");
}

/** The source as the engine is given it, or why it is not drawn. */
export function prepareMermaid(raw: string): MermaidPrepared {
  const source = raw
    .replace(/\r\n?/g, "\n")
    .replace(FRONTMATTER, "")
    .replace(DIRECTIVE, "")
    .replace(CLICK_LINE, "")
    .replace(HTML_TAG, (_full, tag: string) => (tag.toLowerCase() === "br" ? "<br/>" : ""))
    .replace(/\n{3,}/g, "\n\n")
    .trim();
  if (!source) return { ok: false, error: "The diagram is empty." };
  if (source.length > MERMAID_MAX_SOURCE) return { ok: false, error: `The diagram is over ${MERMAID_MAX_SOURCE / 1024} KB, too large to draw.` };
  return { ok: true, source };
}

/** Width and height from the SVG's viewBox (mermaid always writes one); null when it has none. */
export function mermaidSvgSize(svg: string): { width: number; height: number } | null {
  const match = /<svg\b[^>]*\bviewBox\s*=\s*["']\s*([-\d.eE+]+)[\s,]+([-\d.eE+]+)[\s,]+([-\d.eE+]+)[\s,]+([-\d.eE+]+)\s*["']/i.exec(svg);
  if (!match) return null;
  const width = Number(match[3]);
  const height = Number(match[4]);
  return width > 0 && height > 0 && Number.isFinite(width) && Number.isFinite(height) ? { width, height } : null;
}

/** What the engine drew, as it is shown: no script, no handler, no script URL (its viewer runs none anyway). */
export function sanitizeMermaidSvg(svg: string): string {
  return svg
    .replace(/<script\b[\s\S]*?<\/script\s*>/gi, "")
    .replace(/\son[a-z]+\s*=\s*(?:"[^"]*"|'[^']*')/gi, "")
    .replace(/\s(?:href|xlink:href)\s*=\s*(["'])\s*(?:javascript|vbscript|data):[\s\S]*?\1/gi, "");
}

/** The engine's answer to one drawing, checked; null when it is no answer of the engine's. */
export function readMermaidResult(svg: unknown, error: unknown): MermaidResult {
  if (typeof svg === "string" && svg.includes("<svg")) {
    if (svg.length > MERMAID_MAX_SVG) return { ok: false, error: "The diagram is too large to show." };
    const size = mermaidSvgSize(svg);
    if (!size) return { ok: false, error: "The diagram came out with no size." };
    return { ok: true, svg: sanitizeMermaidSvg(svg), ...size };
  }
  const words = typeof error === "string" && error.trim() ? error.trim() : "The diagram could not be drawn.";
  return { ok: false, error: words.slice(0, 1200) };
}

/** What the engine web view says: it is ready (or failed to load), or the answer to one drawing. */
export type MermaidEngineMessage =
  | { type: "ready"; ok: boolean; error?: string }
  | { type: "result"; id: string; result: MermaidResult };

export function parseMermaidEngineMessage(raw: unknown): MermaidEngineMessage | null {
  let value: unknown = raw;
  if (typeof raw === "string") {
    try {
      value = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (!value || typeof value !== "object") return null;
  const message = value as Record<string, unknown>;
  if (message.type === "ready") return { type: "ready", ok: message.ok !== false, error: typeof message.error === "string" ? message.error : undefined };
  if (message.type === "result" && typeof message.id === "string") return { type: "result", id: message.id, result: readMermaidResult(message.ok === false ? undefined : message.svg, message.error) };
  return null;
}

/** The colours a diagram is drawn in: the app's own, light or dark. */
export type MermaidPalette = {
  surface: string;
  ink: string;
  soft: string;
  node: string;
  border: string;
  cluster: string;
};

/** mermaid's `base` theme in the app's colours. */
export function mermaidThemeVariables(mode: MermaidMode, palette: MermaidPalette): Record<string, string | boolean> {
  return {
    darkMode: mode === "dark",
    background: palette.surface,
    fontFamily: "system-ui, -apple-system, 'Segoe UI', Roboto, 'Noto Sans CJK SC', 'PingFang SC', sans-serif",
    fontSize: "14px",
    primaryColor: palette.node,
    primaryTextColor: palette.ink,
    primaryBorderColor: palette.soft,
    secondaryColor: palette.cluster,
    tertiaryColor: palette.surface,
    lineColor: palette.soft,
    textColor: palette.ink,
    mainBkg: palette.node,
    nodeBorder: palette.soft,
    clusterBkg: palette.cluster,
    clusterBorder: palette.border,
    titleColor: palette.ink,
    edgeLabelBackground: palette.surface,
    noteBkgColor: palette.cluster,
    noteTextColor: palette.ink,
    noteBorderColor: palette.border,
    actorBkg: palette.node,
    actorBorder: palette.soft,
    actorTextColor: palette.ink,
    signalColor: palette.ink,
    signalTextColor: palette.ink,
    labelBoxBkgColor: palette.node,
    labelTextColor: palette.ink,
    loopTextColor: palette.ink,
  };
}

/** How the app reaches the engine web view: a message to it. */
export type MermaidEngineLink = { send: (message: string) => void };

type Waiting = { id: string; key: string; source: string; mode: MermaidMode; resolve: (result: MermaidResult) => void };

export type MermaidRenderer = {
  /** A drawing already made for this source and theme. */
  cached(source: string, mode: MermaidMode): MermaidResult | undefined;
  /** The drawing, made once (one at a time) and kept. */
  render(source: string, mode: MermaidMode): Promise<MermaidResult>;
  /** Whether a drawing was ever asked for: the engine web view is made only then. */
  wanted(): boolean;
  /** The engine web view was made (`link`) or went (`null`). */
  attach(link: MermaidEngineLink | null): void;
  /** What the engine web view said. */
  receive(raw: unknown): void;
};

export function createMermaidRenderer(options: {
  themeVariables: (mode: MermaidMode) => Record<string, string | boolean>;
  onWanted?: () => void;
  timeoutMs?: number;
  engineTimeoutMs?: number;
  cacheLimit?: number;
}): MermaidRenderer {
  const cache = new Map<string, MermaidResult>();
  const pending = new Map<string, Promise<MermaidResult>>();
  const queue: Waiting[] = [];
  const limit = options.cacheLimit ?? MERMAID_CACHE_LIMIT;
  let link: MermaidEngineLink | null = null;
  let ready = false;
  let engineFailure: string | null = null;
  let inflight: Waiting | null = null;
  let inflightTimer: ReturnType<typeof setTimeout> | undefined;
  let engineTimer: ReturnType<typeof setTimeout> | undefined;
  let wanted = false;
  let next = 0;
  const keyOf = (source: string, mode: MermaidMode) => `${mode}\n${source}`;

  const keep = (key: string, result: MermaidResult) => {
    cache.delete(key);
    cache.set(key, result);
    while (cache.size > limit) cache.delete(cache.keys().next().value as string);
  };
  const settle = (waiting: Waiting, result: MermaidResult, kept: boolean) => {
    pending.delete(waiting.key);
    if (kept) keep(waiting.key, result);
    waiting.resolve(result);
  };
  const failAll = (error: string) => {
    clearTimeout(engineTimer);
    clearTimeout(inflightTimer);
    const all = [...(inflight ? [inflight] : []), ...queue.splice(0)];
    inflight = null;
    for (const waiting of all) settle(waiting, { ok: false, error }, false);
  };
  const pump = () => {
    if (inflight || !link || !ready || !queue.length) return;
    const waiting = queue.shift()!;
    inflight = waiting;
    inflightTimer = setTimeout(() => {
      if (inflight !== waiting) return;
      inflight = null;
      settle(waiting, { ok: false, error: "Drawing the diagram took too long." }, false);
      pump();
    }, options.timeoutMs ?? MERMAID_RENDER_TIMEOUT_MS);
    try {
      link.send(JSON.stringify({ type: "render", id: waiting.id, source: waiting.source, mode: waiting.mode, themeVariables: options.themeVariables(waiting.mode) }));
    } catch {
      clearTimeout(inflightTimer);
      inflight = null;
      queue.unshift(waiting);
    }
  };

  return {
    cached: (source, mode) => cache.get(keyOf(source, mode)),
    render(source, mode) {
      const key = keyOf(source, mode);
      const hit = cache.get(key);
      if (hit) return Promise.resolve(hit);
      const already = pending.get(key);
      if (already) return already;
      if (engineFailure) return Promise.resolve({ ok: false, error: engineFailure });
      const promise = new Promise<MermaidResult>((resolve) => queue.push({ id: `m${++next}`, key, source, mode, resolve }));
      pending.set(key, promise);
      if (!wanted) {
        wanted = true;
        options.onWanted?.();
      }
      if (!ready && !engineTimer) engineTimer = setTimeout(() => failAll("The diagram engine did not start."), options.engineTimeoutMs ?? MERMAID_ENGINE_TIMEOUT_MS);
      pump();
      return promise;
    },
    wanted: () => wanted,
    attach(next) {
      link = next;
      if (next) return pump();
      // The engine went: what it was drawing is asked again of the next one.
      ready = false;
      clearTimeout(inflightTimer);
      if (inflight) queue.unshift(inflight);
      inflight = null;
    },
    receive(raw) {
      const message = parseMermaidEngineMessage(raw);
      if (!message) return;
      if (message.type === "ready") {
        clearTimeout(engineTimer);
        engineTimer = undefined;
        if (!message.ok) {
          engineFailure = message.error || "The diagram engine did not start.";
          return failAll(engineFailure);
        }
        ready = true;
        return pump();
      }
      if (!inflight || inflight.id !== message.id) return;
      const waiting = inflight;
      clearTimeout(inflightTimer);
      inflight = null;
      settle(waiting, message.result, true);
      pump();
    },
  };
}

/** The engine's script, run after mermaid's own in the engine web view: it draws what the app sends, one at a time. */
export const MERMAID_ENGINE_SCRIPT = String.raw`(function () {
  function post(message) { try { window.nsh.postMessage(JSON.stringify(message)); } catch (_) {} }
  var mermaid = window.mermaid;
  if (!mermaid || typeof mermaid.render !== "function") { post({ type: "ready", ok: false, error: "The diagram engine did not load." }); return; }
  var seq = 0;
  window.addEventListener("message", function (event) {
    var message;
    try { message = JSON.parse(event.data); } catch (_) { return; }
    if (!message || message.type !== "render" || typeof message.id !== "string" || typeof message.source !== "string") return;
    var id = "mewla-mermaid-" + (++seq);
    Promise.resolve().then(function () {
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        suppressErrorRendering: true,
        theme: "base",
        darkMode: message.mode === "dark",
        themeVariables: message.themeVariables || {},
        fontFamily: (message.themeVariables && message.themeVariables.fontFamily) || "sans-serif",
        flowchart: { useMaxWidth: false },
        sequence: { useMaxWidth: false },
        gantt: { useMaxWidth: false },
        state: { useMaxWidth: false },
        class: { useMaxWidth: false },
        er: { useMaxWidth: false }
      });
      return mermaid.render(id, message.source);
    }).then(function (drawn) {
      post({ type: "result", id: message.id, ok: true, svg: drawn && drawn.svg ? String(drawn.svg) : "" });
    }, function (error) {
      var words = error && (error.message || error.str) ? String(error.message || error.str) : String(error || "");
      post({ type: "result", id: message.id, ok: false, error: words });
    }).then(function () {
      var left = document.getElementById("d" + id) || document.getElementById(id);
      if (left && left.parentNode) left.parentNode.removeChild(left);
    });
  });
  post({ type: "ready", ok: true });
})();`;

const ENGINE_CSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";

/** The engine web view's document: mermaid's script (`runtime`), then the engine's. It loads nothing. */
export function mermaidEngineDocument(runtime: string): string {
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${ENGINE_CSP}"><meta name="viewport" content="width=1024"><style>html,body{margin:0;background:transparent}</style></head><body><script>${runtime}</script><script>${MERMAID_ENGINE_SCRIPT}</script></body></html>`;
}

const VIEW_CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; script-src 'none'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";

/**
 * A drawn diagram as a page that runs no script, on `background`. Fitted (`scale` null): the whole diagram inside the
 * view, no larger than drawn. Else at `scale` times its drawn size, scrolled to pan.
 */
export function mermaidSvgDocument(result: { svg: string; width: number; height: number }, background: string, scale: number | null = null): string {
  const color = /^#[0-9a-f]{3,8}$/i.test(background) ? background : "transparent";
  const sized = scale === null
    ? `html,body{height:100%;overflow:hidden}body{display:flex;align-items:center;justify-content:center}svg{display:block;width:100%;height:100%;max-width:${result.width}px;max-height:${result.height}px}`
    : `html,body{overflow:auto}body{padding:16px;box-sizing:border-box;width:max-content}svg{display:block;width:${Math.round(result.width * scale)}px;height:${Math.round(result.height * scale)}px;max-width:none}`;
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${VIEW_CSP}"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;background:${color}}${sized}</style></head><body>${result.svg}</body></html>`;
}
