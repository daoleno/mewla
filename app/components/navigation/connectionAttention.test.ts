import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { connectionAttention } from "./connectionAttention";

const issue = { title: "Mewla Link is offline" } as ConnectionIssue;

describe("connection signal", () => {
  test("healthy and a short reconnect are quiet", () => {
    expect(connectionAttention({ hasServer: true, connection: "connected", issue: null, stalled: false }).badge).toBeNull();
    expect(connectionAttention({ hasServer: true, connection: "connecting", issue: null, stalled: false })).toMatchObject({
      badge: null,
      detail: "Connecting",
    });
  });

  test("a stalled reconnect, Offline and a diagnosed issue put a dot on the menu", () => {
    expect(connectionAttention({ hasServer: true, connection: "connecting", issue: null, stalled: true })).toMatchObject({
      badge: "offline",
      detail: "Reconnecting",
    });
    expect(connectionAttention({ hasServer: true, connection: "offline", issue: null, stalled: false }).badge).toBe("offline");
    expect(connectionAttention({ hasServer: true, connection: "connecting", issue, stalled: false })).toMatchObject({
      badge: "issue",
      detail: "Mewla Link is offline",
      menuLabel: "Open navigation drawer, Mewla Link is offline",
    });
  });

  test("no server is the empty state's job, not a dot", () => {
    expect(connectionAttention({ hasServer: false, connection: "offline", issue: null, stalled: true }).badge).toBeNull();
  });

  test("the menu dot and the footer line read the same rule", () => {
    for (const file of ["PrimaryDrawerShell.tsx", "PrimaryDrawerPanel.tsx"]) {
      const source = readFileSync(new URL(`./${file}`, import.meta.url), "utf8");
      expect(source).toContain("useConnectionAttention()");
      expect(source).not.toContain("serverConnectionIssues");
    }
  });
});
