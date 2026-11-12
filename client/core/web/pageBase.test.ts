import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// R5#2: a reload of /sessions/<id> loaded the page's files from /sessions/ (404) and spun forever.
// The page needs <base href="/">; scripts/build-web-ui.sh used to add it unless the page already
// held "<base ", and NSH 0.5.0's page has that text in a comment, so nothing was added.
const root = join(import.meta.dir, "../../..");

describe("the web page's base (R5#2)", () => {
  test("nsh package writes <base href=\"/\"> (nsh.json web.base)", () => {
    const config = JSON.parse(readFileSync(join(root, "client/ui/nsh.json"), "utf8"));
    expect(config.web?.base).toBe("/");
  });

  test("the daemon's web build refuses a page without it, and does not patch the page itself", () => {
    const script = readFileSync(join(root, "scripts/build-web-ui.sh"), "utf8");
    expect(script).toContain(`grep -q '^<base href="/">$' "$APP/index.html"`);
    expect(script).not.toContain('"<base " not in page');
  });
});
