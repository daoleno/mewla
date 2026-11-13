import { describe, expect, test } from "bun:test";
import { brainRowLine, paneSelection, paneStateLine, paneTrayTitle, requestPaneKey, rowKey } from "./pane";
import type { TrayRequest, TrayRow } from "./model";

const row = (key: string, quiet?: boolean): TrayRow => ({ key, title: key, line: "", end: "", tile: { kind: "brain" }, target: { open: "work", workId: key }, quiet });
const request = (key: string): TrayRequest => ({ key, kind: "prompt", where: "session", title: key, source: "s" });

describe("the left pane's header (SPEC §14.1)", () => {
  test("the state line says the connection, and Brain's own trouble", () => {
    expect(paneStateLine({ kind: "quiet" })).toEqual({ words: "Online", dot: "ink", ink: false });
    expect(paneStateLine({ kind: "needs" }).words).toBe("Online");
    expect(paneStateLine({ kind: "running" }).words).toBe("Online");
    expect(paneStateLine({ kind: "connecting" })).toEqual({ words: "Connecting…", dot: "ring", ink: false });
    expect(paneStateLine({ kind: "offline", diagnosis: "Relay unreachable" })).toEqual({ words: "Offline · Relay unreachable", dot: "ring", ink: false });
    expect(paneStateLine({ kind: "offline" }).words).toBe("Offline");
    expect(paneStateLine({ kind: "pair" })).toEqual({ words: "Pair again", dot: "seal", ink: true });
    expect(paneStateLine({ kind: "starting" })).toEqual({ words: "Brain is starting", dot: null, ink: false });
    expect(paneStateLine({ kind: "cant_start" })).toEqual({ words: "Brain can't start", dot: "seal", ink: false });
  });
});

describe("the tray's title in the pane", () => {
  test("what runs and what needs you, together; queued Work is not running", () => {
    expect(paneTrayTitle({ running: [], needs: [] })).toBe("All quiet");
    expect(paneTrayTitle({ running: [row("a"), row("b"), row("q", true)], needs: [] })).toBe("2 running");
    expect(paneTrayTitle({ running: [], needs: [request("r")] })).toBe("1 needs you");
    const running = Array.from({ length: 12 }, (_, i) => row(`w${i}`));
    expect(paneTrayTitle({ running, needs: [request("a"), request("b"), request("c")] })).toBe("12 running, 3 need you");
  });
});

describe("the selected row", () => {
  test("Brain with nothing above it; else the page right above Brain, whatever it pushed", () => {
    expect(paneSelection([{ name: "home" }])).toBe("brain");
    expect(paneSelection([{ name: "home" }, { name: "session", workerId: "w1" }])).toBe("session:w1");
    expect(paneSelection([{ name: "home" }, { name: "session", workerId: "w1" }, { name: "git" }])).toBe("session:w1");
    expect(paneSelection([{ name: "home" }, { name: "workDetail", workId: "k1" }])).toBe("work:k1");
    expect(paneSelection([{ name: "home" }, { name: "item", id: "c1" }])).toBe("item:c1");
    expect(paneSelection([{ name: "home" }, { name: "calendar" }, { name: "item", id: "c1" }])).toBe("calendar");
    expect(paneSelection([{ name: "home" }, { name: "allSessions" }])).toBe("all");
    expect(paneSelection([{ name: "home" }, { name: "computer" }, { name: "settings" }])).toBe("computer");
    expect(paneSelection([{ name: "home" }, { name: "files-workspace" }])).toBeNull();
  });

  test("a row's key is the page it opens", () => {
    expect(rowKey({ target: { open: "session", workerId: "w1" } })).toBe("session:w1");
    expect(rowKey({ target: { open: "work", workId: "k1" } })).toBe("work:k1");
    expect(rowKey({ target: { open: "item", itemId: "c1" } })).toBe("item:c1");
  });
});

describe("the Brain row", () => {
  test("the last thing Brain said, on one line, without markdown", () => {
    expect(brainRowLine(null)).toBe("");
    expect(brainRowLine([{ kind: "user_message", seq: 1, body: "hi" }])).toBe("");
    expect(brainRowLine([
      { kind: "assistant_message", seq: 3, body: "## Done\n- shipped **v2** to [staging](http://x)\n" },
      { kind: "assistant_message", seq: 2, body: "older" },
      { kind: "user_message", seq: 4, body: "thanks" },
    ])).toBe("Done shipped v2 to staging");
  });
});

describe("a Needs you row's page", () => {
  test("a Session's or a Work's ask and a Calendar alert own the page they open; Brain's card and a new device own none", () => {
    expect(requestPaneKey({ where: "session", workerId: "w1" })).toBe("session:w1");
    expect(requestPaneKey({ where: "work", workId: "k1" })).toBe("work:k1");
    expect(requestPaneKey({ where: "alert", itemId: "c1" })).toBe("item:c1");
    expect(requestPaneKey({ where: "brain", itemId: "c1" })).toBeNull();
    expect(requestPaneKey({ where: "device" })).toBeNull();
    // The same keys paneSelection gives the pages they open.
    expect(paneSelection([{ name: "home" }, { name: "session", workerId: "w1" }])).toBe(requestPaneKey({ where: "session", workerId: "w1" }));
  });
});
