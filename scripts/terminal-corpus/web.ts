// The render corpus on the web client's terminal page (client/ui/web/terminal: libghostty-vt as
// WebAssembly on a canvas), the way the app runs it: under the app's Content-Security-Policy, in a
// frame sandboxed like NSH's web view (srcdoc, an opaque origin), at a real device pixel ratio. Each
// screen is written into the page at its grid; one PNG per screen, the screen's own pixels.
//
//   bun scripts/terminal-corpus/web.ts <out dir> [name...]
//
// Needs playwright-core importable (PLAYWRIGHT_CORE: a path to it) and a browser: ENGINE chromium
// (default), firefox or webkit; BROWSER (or CHROME): its binary, else Playwright's own. DPR (default 2.625, a 1080 px wide phone) and 13px text. The VT is
// scripts/build-libghostty-web.sh's (built once, then cached), or WASM: a mewla-vt.wasm.
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { terminalPageHtml, webTheme, WEB_TERMINAL_FONT } from "../../client/core/terminal/webSurface";

const here = import.meta.dir;
const root = resolve(here, "../..");
const out = resolve(process.argv[2] ?? "web-shots");
mkdirSync(out, { recursive: true });
const corpus = JSON.parse(readFileSync(join(here, "corpus.json"), "utf8")) as Record<string, { cols: number; rows: number }>;
const names = process.argv.length > 3 ? process.argv.slice(3) : Object.keys(corpus);
const dpr = Number(process.env.DPR ?? 2.625);
const wasm = process.env.WASM ?? execFileSync(join(root, "scripts/build-libghostty-web.sh"), { encoding: "utf8" }).trim().split("\n").pop()!;
const engine = process.env.ENGINE ?? "chromium";
const playwright = await import(process.env.PLAYWRIGHT_CORE ?? "playwright-core");

const PALETTE = ["#1d1b19", "#e0685a", "#8fbf7f", "#e3b55f", "#7aa2f7", "#c49bd8", "#6fc1c1", "#d8d2c8", "#57514a", "#ff8a7a", "#a8d898", "#f2cb7c", "#9ab6ff", "#d9b2ea", "#8ed8d8", "#f4f0ea"];
const theme = webTheme({ background: "#141210", foreground: "#F4F0EA", cursor: "#F4F0EA", selection: "#3D7EFF55", palette: PALETTE });
// NSH puts its bridge (an inline script, which the policy allows in its frames) first in the frame's head.
const bridge = `<script>window.nsh={postMessage:function(d){parent.postMessage({fromTerminal:d},"*")}}</script>`;
const frameHtml = terminalPageHtml("/terminal/").replace("<head>", "<head>" + bridge);
const appHtml = `<!doctype html><html><head><meta charset="utf-8"></head><body style="margin:0;background:#000">
<script src="/app.js"></script></body></html>`;
const appJs = `window.__ev = [];
addEventListener("message", (e) => { if (e.data && e.data.fromTerminal) window.__ev.push(JSON.parse(e.data.fromTerminal)); });
const frame = document.createElement("iframe");
frame.id = "term";
frame.style.cssText = "position:fixed;left:0;top:0;border:0;width:400px;height:400px";
document.body.append(frame);
frame.setAttribute("sandbox", "allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads allow-scripts");
frame.srcdoc = ${JSON.stringify(frameHtml)};
window.post = (batch) => document.getElementById("term").contentWindow.postMessage(JSON.stringify(batch), "*");`;

// The app's policy (daemon/server/web_ui.go, which names the page's own origin for WebKit), and the
// terminal page's files readable from its opaque origin. The frame is in the document before it gets
// its sandbox and page, the order in which WebKit refuses most without that origin.
const server = Bun.serve({
  port: 0,
  hostname: "127.0.0.1",
  fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname;
    const origin = url.origin;
    const headers: Record<string, string> = {
      "Content-Security-Policy": `default-src 'self'; script-src 'self' ${origin} 'wasm-unsafe-eval' 'unsafe-inline' blob:; style-src 'self' 'unsafe-inline'; ` +
        `img-src 'self' data: blob: https:; font-src 'self' ${origin} data:; connect-src 'self' ${origin}; frame-src 'none'; object-src 'none'; base-uri 'self'; form-action 'none'`,
      "X-Content-Type-Options": "nosniff",
      "Cross-Origin-Opener-Policy": "same-origin",
    };
    if (path.startsWith("/terminal/")) headers["Access-Control-Allow-Origin"] = "*";
    if (path === "/") return new Response(appHtml, { headers: { ...headers, "Content-Type": "text/html" } });
    if (path === "/favicon.ico") return new Response(null, { status: 204 });
    if (path === "/app.js") return new Response(appJs, { headers: { ...headers, "Content-Type": "text/javascript" } });
    const name = path.split("/").pop()!;
    if (name === "terminal.js") return new Response(readFileSync(join(root, "client/ui/web/terminal/terminal.js")), { headers: { ...headers, "Content-Type": "text/javascript" } });
    if (name === "mewla-vt.wasm") return new Response(readFileSync(wasm), { headers: { ...headers, "Content-Type": "application/wasm" } });
    if (name.endsWith(".ttf")) return new Response(readFileSync(join(root, "client/native/terminal/fonts", name)), { headers: { ...headers, "Content-Type": "font/ttf" } });
    return new Response("not found", { status: 404, headers });
  },
});

// A real device scale. In Chromium the frame is another origin, so another process: only Chromium's own
// scale reaches it. Firefox's emulated scale changes devicePixelRatio but not its device-pixel boxes, so
// it takes the scale a HiDPI screen gives it (the pref). WebKit keeps the frame in the page's process.
const browser = await playwright[engine].launch({
  executablePath: process.env.BROWSER ?? process.env.CHROME,
  args: engine === "chromium" ? [`--force-device-scale-factor=${dpr}`] : [],
  ...(engine === "firefox" ? { firefoxUserPrefs: { "layout.css.devPixelsPerPx": String(dpr) } } : {}),
});
const failures: string[] = [];
for (const name of names) {
  const { cols, rows } = corpus[name];
  const page = await browser.newPage({ viewport: { width: 1200, height: 1600 }, ...(engine === "webkit" ? { deviceScaleFactor: dpr } : {}) });
  page.on("pageerror", (e: Error) => failures.push(`${name}: ${e.message}`));
  page.on("console", (m: any) => { if (m.type() === "error") failures.push(`${name}: ${m.text()}`); });
  await page.goto(`http://127.0.0.1:${server.port}/`);
  const events = () => page.evaluate(() => (window as any).__ev as any[]);
  await page.waitForFunction(() => (window as any).__ev.some((e: any) => e.type === "ready"), null, { timeout: 15000 })
    .catch(() => { throw new Error(`${name}: the page never said ready\n${failures.join("\n")}`); });
  await page.evaluate((batch: unknown[]) => (window as any).post(batch), [{ type: "theme", theme }, { type: "font", size: 13, family: WEB_TERMINAL_FONT }]);
  await page.waitForFunction(() => (window as any).__ev.some((e: any) => e.type === "size" || e.type === "failed"), null, { timeout: 15000 });
  const failed = (await events()).find((e) => e.type === "failed");
  if (failed) throw new Error(`${name}: ${failed.message}`);
  const frame = page.frames().find((f: any) => f !== page.mainFrame())!;
  await frame.evaluate(async () => { await document.fonts.ready; });
  await page.waitForTimeout(200);
  // The grid as the page measured it, then the frame sized to exactly cols x rows of it.
  const probe = async () => {
    await page.evaluate(() => (window as any).post([{ type: "probe" }]));
    await page.waitForTimeout(100);
    return (await events()).filter((e) => e.type === "probe").pop();
  };
  const m = /cell (\d+)x(\d+) dpr ([\d.]+)/.exec((await probe()).grid)!;
  const [cw, ch, ratio] = [Number(m[1]), Number(m[2]), Number(m[3])];
  // Firefox's ratios are whole app units per pixel (2.625 is 60/23, 2.6087): close is the asked one.
  if (Math.abs(ratio - dpr) > 0.05) throw new Error(`${name}: the page measured at dpr ${ratio}, want ${dpr}`);
  const padX = Math.round(6 * ratio), padY = Math.round(4 * ratio);
  await page.evaluate(([w, h]: number[]) => {
    const f = document.getElementById("term")!;
    f.style.width = w + "px";
    f.style.height = h + "px";
  }, [(padX + (cols + 0.5) * cw) / ratio, (padY + (rows + 0.5) * ch) / ratio]);
  await page.waitForFunction(([c, r]: number[]) => {
    const size = (window as any).__ev.filter((e: any) => e.type === "size").pop();
    return size && size.cols === c && size.rows === r;
  }, [cols, rows], { timeout: 5000 }).catch(() => {});
  const size = (await events()).filter((e) => e.type === "size").pop();
  await page.evaluate((batch: unknown[]) => (window as any).post(batch), [{ type: "write", data: readFileSync(join(here, name + ".vt"), "utf8") }]);
  await page.waitForTimeout(300);
  // The screen's own pixels (the canvas, cropped to the grid): what the seam check reads.
  const png = await frame.evaluate(([x, y, w, h]: number[]) => {
    const source = document.getElementById("vt") as HTMLCanvasElement;
    const crop = document.createElement("canvas");
    crop.width = w;
    crop.height = h;
    crop.getContext("2d")!.drawImage(source, x, y, w, h, 0, 0, w, h);
    return crop.toDataURL("image/png");
  }, [padX, padY, cols * cw, rows * ch]);
  writeFileSync(join(out, name + ".png"), Buffer.from(png.split(",")[1], "base64"));
  console.log(name, `${size?.cols}x${size?.rows}`, `cell ${cw}x${ch} at dpr ${ratio}`);
  if (size?.cols !== cols || size?.rows !== rows) failures.push(`${name}: grid ${size?.cols}x${size?.rows}, want ${cols}x${rows}`);
  await page.close();
}
await browser.close();
server.stop();
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
