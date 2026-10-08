import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const appRoot = join(import.meta.dir, "../..");
const notice = readFileSync(join(import.meta.dir, "ServerOfflineNotice.tsx"), "utf8");

describe("connection status has one home", () => {
  test("pages name the server only while it is offline", () => {
    expect(notice).toContain('if (connection !== "offline") return null;');
    expect(notice).not.toContain("StatusPill");
  });

  test("no page renders a standing server header", () => {
    const files = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "--", "."], { cwd: appRoot, encoding: "utf8" })
      .split("\n")
      .filter((path) => /\.(?:ts|tsx)$/.test(path) && !/\.test\./.test(path));
    const hits = files.filter((path) => {
      try {
        return readFileSync(join(appRoot, path), "utf8").includes("ServerContextRow");
      } catch {
        return false;
      }
    });
    expect(hits).toEqual([]);
  });
});
