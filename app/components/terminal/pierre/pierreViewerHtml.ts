// The WebView document around a generated Pierre runtime: no network, no
// navigation, one inline script. The terminal font is the only file it may
// read, so code looks the same here as in the terminal.

export const PIERRE_FONT_FAMILY = "MewlaMono";

export interface PierreViewerHtmlOptions {
  /** The generated runtime (an IIFE that mounts itself on document.body). */
  source: string;
  /** file:// or https URI of the bundled terminal font; null falls back to monospace. */
  fontUri: string | null;
  background: string;
}

export function buildPierreViewerHtml({ source, fontUri, background }: PierreViewerHtmlOptions): string {
  const script = source.replace(/<\/(script)/gi, "<\\/$1");
  const font = fontUri
    ? `@font-face{font-family:'${PIERRE_FONT_FAMILY}';src:url('${fontUri.replace(/\\/g, "\\\\").replace(/'/g, "\\'")}') format('truetype');font-display:swap}`
    : "";
  // `background` is a theme colour, never user text, but it is still escaped for CSS.
  const color = background.replace(/[^#0-9a-zA-Z(),.% -]/g, "");
  return `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; font-src file: https: data:; img-src data: blob:; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; form-action 'none'; base-uri 'none'">
<style>${font}html,body{margin:0;width:100%;height:100%;overflow:hidden;background:${color};-webkit-text-size-adjust:none;text-size-adjust:none;-webkit-tap-highlight-color:transparent}body{position:relative}</style>
</head><body><script>${script}</script></body></html>`;
}
