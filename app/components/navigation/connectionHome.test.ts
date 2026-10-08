import { describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const appRoot = join(import.meta.dir, "../..");

function sourceFiles() {
  return execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "--", "."], { cwd: appRoot, encoding: "utf8" })
    .split("\n")
    .filter((path) => /\.(?:ts|tsx)$/.test(path) && !/\.test\./.test(path));
}

function filesContaining(needle: string) {
  return sourceFiles().filter((path) => {
    try {
      return readFileSync(join(appRoot, path), "utf8").includes(needle);
    } catch {
      return false;
    }
  });
}

describe("connection status has one home", () => {
  // The dot on ☰ and the menu footer line say the server is unreachable;
  // pages, chats and lists carry no connection banner of their own.
  test("no page renders a connection banner or a standing server header", () => {
    for (const banner of [
      "ConnectionPathIndicator",
      "ServerOfflineNotice",
      "SessionsOverview",
      "ServerContextRow",
    ]) {
      expect({ banner, hits: filesContaining(banner) }).toEqual({ banner, hits: [] });
    }
  });

  test("Brain has no standing strip above the conversation", () => {
    expect(filesContaining("BrainWorkHeader")).toEqual([]);
  });
});
