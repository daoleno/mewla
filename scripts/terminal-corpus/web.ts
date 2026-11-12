// The render corpus on the web client's terminal page (client/ui/web/terminal, xterm.js): each
// screen written into the real page at its grid, one PNG per screen.
//
//   bun scripts/terminal-corpus/web.ts <out dir> [name...]
//
// Needs playwright-core importable (PLAYWRIGHT_CORE: a path to it) and a Chromium
// (CHROME: its binary). DPR 2.625 and 13px text: a 1080 px wide phone.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { terminalPageHtml, webTheme, WEB_TERMINAL_FONT } from "../../client/core/terminal/webSurface";

const here = import.meta.dir;
const root = resolve(here, "../..");
const out = resolve(process.argv[2] ?? "web-shots");
mkdirSync(out, { recursive: true });
const corpus = JSON.parse(readFileSync(join(here, "corpus.json"), "utf8")) as Record<string, { cols: number; rows: number }>;
const names = process.argv.length > 3 ? process.argv.slice(3) : Object.keys(corpus);
const { chromium } = await import(process.env.PLAYWRIGHT_CORE ?? "playwright-core");

const PALETTE = ["#1d1b19", "#e0685a", "#8fbf7f", "#e3b55f", "#7aa2f7", "#c49bd8", "#6fc1c1", "#d8d2c8", "#57514a", "#ff8a7a", "#a8d898", "#f2cb7c", "#9ab6ff", "#d9b2ea", "#8ed8d8", "#f4f0ea"];
const theme = webTheme({ background: "#141210", foreground: "#F4F0EA", cursor: "#F4F0EA", selection: "#3D7EFF55", palette: PALETTE });
const family = process.env.FONT_FAMILY ?? WEB_TERMINAL_FONT;
const capture = `<script>window.nsh={postMessage:function(d){(window.__ev=window.__ev||[]).push(JSON.parse(d))}}</script>`;
const html = terminalPageHtml("/terminal/").replace("<script data-xterm", capture + "<script data-xterm");

const browser = await chromium.launch({ executablePath: process.env.CHROME });
for (const name of names) {
  const { cols, rows } = corpus[name];
  const page = await browser.newPage({ viewport: { width: 700, height: 1400 }, deviceScaleFactor: 2.625 });
  await page.route("http://corpus.test/**", async (route: any) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/") return route.fulfill({ contentType: "text/html", body: html });
    const file = join(root, "client/ui/web", path);
    route.fulfill({ body: readFileSync(file), contentType: path.endsWith(".css") ? "text/css" : "text/javascript" });
  });
  await page.goto("http://corpus.test/");
  const post = (batch: unknown[]) => page.evaluate((data: string) => window.postMessage(data, "*"), JSON.stringify(batch));
  await page.waitForFunction(() => (window as any).__ev?.some((e: any) => e.type === "ready"));
  await post([{ type: "theme", theme }, { type: "font", size: 13, family }]);
  await page.waitForFunction(() => (window as any).__ev?.some((e: any) => e.type === "size"));
  // Fit the page to exactly cols x rows.
  const cell = await page.evaluate(() => {
    const s = document.querySelector(".xterm-screen") as HTMLElement;
    const size = (window as any).__ev.filter((e: any) => e.type === "size").pop();
    return { w: s.getBoundingClientRect().width / size.cols, h: s.getBoundingClientRect().height / size.rows };
  });
  await page.setViewportSize({ width: Math.ceil(6 + cols * cell.w + 0.5), height: Math.ceil(4 + rows * cell.h + 0.5) });
  await page.waitForFunction(([c, r]: number[]) => (window as any).__ev.filter((e: any) => e.type === "size").pop()?.cols === c &&
    (window as any).__ev.filter((e: any) => e.type === "size").pop()?.rows === r, [cols, rows], { timeout: 5000 }).catch(() => {});
  const got = await page.evaluate(() => (window as any).__ev.filter((e: any) => e.type === "size").pop());
  await post([{ type: "write", data: readFileSync(join(here, name + ".vt"), "utf8") }]);
  await page.waitForTimeout(400);
  writeFileSync(join(out, name + ".png"), await page.locator(".xterm-screen").screenshot());
  console.log(name, `${got.cols}x${got.rows}`, `cell ${cell.w.toFixed(3)}x${cell.h.toFixed(3)}`);
  await page.close();
}
await browser.close();
