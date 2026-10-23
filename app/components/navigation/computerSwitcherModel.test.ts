import { describe, expect, test } from "bun:test";
import type { StoredServer } from "../../services/storedServerContract";
import {
  attentionTone,
  computerRowLabel,
  computerRowTap,
  computerSwitcherRows,
} from "./computerSwitcherModel";

function server(id: string, name: string): StoredServer {
  return { id, name, url: `https://${id}.example`, daemonId: `d-${id}`, daemonPublicKey: "k" };
}

const healthy = { badge: null, detail: "Connected" } as const;

describe("computer row tap", () => {
  test("goes to Settings when there is none or one, opens the switcher for several", () => {
    expect(computerRowTap(0)).toBe("settings");
    expect(computerRowTap(1)).toBe("settings");
    expect(computerRowTap(2)).toBe("switcher");
    expect(computerRowTap(5)).toBe("switcher");
  });
});

describe("computer switcher rows", () => {
  test("one computer: in use, quiet when healthy", () => {
    const rows = computerSwitcherRows({
      servers: [server("a", "Studio Mac")],
      currentServerId: "a",
      attention: healthy,
      checks: {},
    });
    expect(rows).toEqual([{ id: "a", name: "Studio Mac", current: true, status: "", tone: "quiet" }]);
  });

  test("several computers keep Settings' order and mark only the current one", () => {
    const rows = computerSwitcherRows({
      servers: [server("a", "Studio Mac"), server("b", "Homelab"), server("c", "Laptop")],
      currentServerId: "b",
      attention: healthy,
      checks: { a: { state: "online" }, c: { state: "online", name: "Laptop" } },
    });
    expect(rows.map((row) => [row.name, row.current, row.status])).toEqual([
      ["Studio Mac", false, "Online"],
      ["Homelab", true, ""],
      ["Laptop", false, "Online"],
    ]);
  });

  test("an offline computer is colored; one that doesn't trust this phone is an issue", () => {
    const rows = computerSwitcherRows({
      servers: [server("a", "Studio Mac"), server("b", "Homelab"), server("c", "Laptop")],
      currentServerId: "a",
      attention: healthy,
      checks: { b: { state: "offline" }, c: { state: "unpaired" } },
    });
    expect(rows[1]).toMatchObject({ status: "Offline", tone: "offline" });
    expect(rows[2]).toMatchObject({ status: "Pair again", tone: "issue" });
  });

  test("a computer not asked yet says nothing", () => {
    const rows = computerSwitcherRows({
      servers: [server("a", "Studio Mac"), server("b", "Homelab")],
      currentServerId: "a",
      attention: healthy,
      checks: {},
    });
    expect(rows[1]).toMatchObject({ status: "", tone: "quiet" });
  });

  test("the current one says what the menu row says, in the same tone", () => {
    const offline = computerSwitcherRows({
      servers: [server("a", "Studio Mac"), server("b", "Homelab")],
      currentServerId: "a",
      attention: { badge: "offline", detail: "Reconnecting" },
      checks: { a: { state: "online" } },
    });
    expect(offline[0]).toMatchObject({ current: true, status: "Reconnecting", tone: "offline" });

    const issue = computerSwitcherRows({
      servers: [server("a", "Studio Mac")],
      currentServerId: "a",
      attention: { badge: "issue", detail: "Pair again" },
      checks: {},
    });
    expect(issue[0]).toMatchObject({ status: "Pair again", tone: "issue" });
  });

  test("a current id that matches no computer marks none", () => {
    const rows = computerSwitcherRows({
      servers: [server("a", "Studio Mac"), server("b", "Homelab")],
      currentServerId: "gone",
      attention: healthy,
      checks: {},
    });
    expect(rows.some((row) => row.current)).toBe(false);
  });
});

describe("tone and label", () => {
  test("tone follows the ☰ dot", () => {
    expect(attentionTone("issue")).toBe("issue");
    expect(attentionTone("offline")).toBe("offline");
    expect(attentionTone(null)).toBe("quiet");
  });

  test("the spoken label carries name, state and use", () => {
    expect(computerRowLabel({ name: "Homelab", status: "Offline", current: false })).toBe("Homelab, Offline");
    expect(computerRowLabel({ name: "Studio Mac", status: "", current: true })).toBe("Studio Mac, in use");
  });
});
