import { describe, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// R5#2: a reload of /sessions/<id> loaded the page's files from /sessions/ (404) and spun forever.
// The page must name every file from the site's root. NSH 0.5 did it with <base href="/">; NSH 0.6
// writes no <base> (a strict base-uri refuses it) and names each file by an absolute URL under
// nsh.json web.base, so scripts/web-page-root.py checks that, and the daemon's web build runs it.
const root = join(import.meta.dir, "../../..");
const checker = join(root, "scripts/web-page-root.py");

/** A page as NSH 0.6 packages it, with `file` as one of the files it names. */
const page = (file: string, base = "/") => [
  "<!doctype html>",
  `<link rel="stylesheet" href="${file}">`,
  '<script type="application/json" id="nsh-fetch">["/nsh.0123456789.wasm","/app.0123456789.js"]</script>',
  `<script type="application/json" id="nsh-app">{"base":"${base}","code":"/app.0123456789.js"}</script>`,
  '<script src="/nsh-start.0123456789.js"></script>',
].join("\n");

function check(html: string): number {
  const dir = mkdtempSync(join(tmpdir(), "page-root-"));
  try {
    writeFileSync(join(dir, "index.html"), html);
    return spawnSync("python3", ["-I", checker, join(dir, "index.html")], { stdio: "ignore" }).status ?? -1;
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

describe("the web page loads its files from the site's root (R5#2)", () => {
  test("nsh.json web.base is \"/\"", () => {
    const config = JSON.parse(readFileSync(join(root, "client/ui/nsh.json"), "utf8"));
    expect(config.web?.base).toBe("/");
  });

  test("a page naming its files from the root passes", () => {
    expect(check(page("/nsh-page.0123456789.css"))).toBe(0);
  });

  test("a file named relative to the page, another base, or a <base> element fails", () => {
    expect(check(page("nsh-page.0123456789.css"))).not.toBe(0);
    expect(check(page("//cdn.example/x.css"))).not.toBe(0);
    expect(check(page("/nsh-page.0123456789.css", "/app/"))).not.toBe(0);
    expect(check(page("/nsh-page.0123456789.css").replace("<!doctype html>", '<!doctype html>\n<base href="/">'))).not.toBe(0);
  });

  test("the daemon's web build and its prebuilt check run it", () => {
    expect(readFileSync(join(root, "scripts/build-web-ui.sh"), "utf8")).toContain('scripts/web-page-root.py" "$APP/index.html"');
    expect(readFileSync(join(root, "scripts/build-daemon-linux.sh"), "utf8")).toContain('scripts/web-page-root.py" "$WEB_DIST/index.html.gz"');
  });
});
