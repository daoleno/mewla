import { SCREEN_VIEWER_SOURCE } from "./screenViewerSource";

// Pixel-only document with no network access: the host relays RFB bytes.
// Transparent, so the host's themed backdrop letterboxes the remote screen.
export function buildScreenViewerHtml(): string {
  const source = SCREEN_VIEWER_SOURCE.replace(/<\/(script)/gi, "<\\/$1");
  return `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; form-action 'none'; base-uri 'none'">
<style>html,body{margin:0;width:100%;height:100%;background:transparent;overflow:hidden}</style>
</head><body><script type="module">${source}</script></body></html>`;
}
