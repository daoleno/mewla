import { describe, expect, test } from "bun:test";
import { poppedPage, reuseRoutes, WebHistory } from "./history";

const at = (history: WebHistory<string>, key: string, serverId: string | null = "s1") =>
  history.visit({ key, path: `/${key}`, title: `${key} · Mewla`, serverId, snapshot: key });

describe("web history", () => {
  test("the first page rewrites the entry the page loaded on; new pages push", () => {
    const history = new WebHistory<string>();
    expect(at(history, "brain").mode).toBe("replace");
    const calendar = at(history, "calendar");
    expect(calendar.mode).toBe("push");
    expect(calendar.state).toEqual({ index: 1, key: "calendar", serverId: "s1", snapshot: "calendar" });
    expect(calendar.path).toBe("/calendar");
    expect(calendar.title).toBe("calendar · Mewla");
  });

  test("the same page again only rewrites its entry (a new title)", () => {
    const history = new WebHistory<string>();
    at(history, "brain");
    at(history, "list");
    expect(at(history, "list").mode).toBe("replace");
    expect(history.position()).toBe(1);
  });

  test("going back in the app steps the browser back, so Forward still works", () => {
    const history = new WebHistory<string>();
    at(history, "list");
    at(history, "session");
    expect(at(history, "list").mode).toBe("back");
    expect(history.position()).toBe(0);
    expect(at(history, "session").mode).toBe("forward");
    expect(history.position()).toBe(1);
  });

  test("somewhere new after going back drops the entries ahead", () => {
    const history = new WebHistory<string>();
    at(history, "a");
    at(history, "b");
    at(history, "c");
    at(history, "b");
    expect(at(history, "d").mode).toBe("push");
    // `c` is gone: coming back to it is new again, not a Forward.
    at(history, "b");
    expect(at(history, "c").mode).toBe("push");
  });

  test("the browser's Back and Forward move the mirror; the page they show is then a rewrite", () => {
    const history = new WebHistory<string>();
    at(history, "a");
    const b = at(history, "b");
    at(history, "c");
    expect(history.popped(b.state)).toEqual(b.state);
    expect(history.position()).toBe(1);
    expect(at(history, "b").mode).toBe("replace");
    expect(history.popped({ nsh: 0 })).toBeNull();
    expect(history.popped(null)).toBeNull();
  });

  test("a reload starts on the entry it kept", () => {
    const history = new WebHistory<string>();
    history.start({ index: 4, key: "calendar", serverId: "s1", snapshot: "calendar" });
    const first = at(history, "calendar");
    expect(first.mode).toBe("replace");
    expect(first.state.index).toBe(4);
    expect(at(history, "stats").state.index).toBe(5);
  });
});

describe("restoring an entry's routes", () => {
  test("a route already shown at its place stays the same value; the rest are the entry's", () => {
    const list = { name: "sessions" };
    const session = { name: "session", workerId: "w1" };
    const restored = reuseRoutes([list, session], [{ name: "sessions" }, { name: "session", workerId: "w2" }, { name: "git" }]);
    expect(restored[0]).toBe(list);
    expect(restored[1]).not.toBe(session);
    expect(restored).toEqual([{ name: "sessions" }, { name: "session", workerId: "w2" }, { name: "git" }]);
    expect(reuseRoutes([list, session], [{ name: "sessions" }])[0]).toBe(list);
  });
});

describe("the page an entry reopens", () => {
  const entry = (serverId: string | null) => ({ index: 1, key: "k", serverId, snapshot: { tab: 1 } });

  test("an entry of this computer reopens what it showed", () => {
    expect(poppedPage({ path: "/sessions/w1", state: entry("s1") }, "s1")).toEqual({ kind: "snapshot", snapshot: { tab: 1 } });
  });

  test("another computer's Session opens Sessions, never its id", () => {
    expect(poppedPage({ path: "/sessions/w1", state: entry("s2") }, "s1")).toEqual({ kind: "place", place: { key: "sessions" } });
    expect(poppedPage({ path: "/calendar", state: entry("s2") }, "s1")).toEqual({ kind: "place", place: { key: "calendar" } });
  });

  test("without the app's state the path decides; an unknown one opens Brain", () => {
    expect(poppedPage({ path: "/sessions/w1", state: null }, "s1")).toEqual({ kind: "place", place: { key: "session", workerId: "w1" } });
    expect(poppedPage({ path: "/nowhere", state: { nsh: 2 } }, "s1")).toEqual({ kind: "place", place: { key: "brain" } });
  });
});
