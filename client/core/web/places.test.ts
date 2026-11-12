import { describe, expect, test } from "bun:test";
import {
  clampPanelWidth,
  documentTitle,
  isDesktopLayout,
  pageColumnWidth,
  placeOfPath,
  placePath,
  sidebarKeyOf,
  sidebarRows,
  storedSidebarWidth,
  type WebPlace,
} from "./places";

describe("desktop layout gate", () => {
  test("a non-phone window gets the desktop layout from 1024 pt; phones never do", () => {
    expect(isDesktopLayout({ mobile: false, width: 1024 })).toBe(true);
    expect(isDesktopLayout({ mobile: false, width: 1023 })).toBe(false);
    expect(isDesktopLayout({ mobile: true, width: 1366 })).toBe(false);
  });
});

describe("sidebar", () => {
  test("the rows in order; Friends only where it is on", () => {
    expect(sidebarRows({ friends: true }).map((row) => row.label)).toEqual(["Brain", "Sessions", "Calendar", "Extensions", "Screen", "Stats", "Resources", "Friends", "Settings"]);
    expect(sidebarRows({ friends: false }).map((row) => row.key)).not.toContain("friends");
  });

  test("a Session selects Sessions, a Work file Brain, the computer page Settings", () => {
    expect(sidebarKeyOf({ key: "session", workerId: "w1" })).toBe("sessions");
    expect(sidebarKeyOf({ key: "work", page: { name: "workFile", id: "w1" } })).toBe("brain");
    expect(sidebarKeyOf({ key: "computer" })).toBe("settings");
    expect(sidebarKeyOf({ key: "calendar" })).toBe("calendar");
  });
});

describe("paths", () => {
  const places: WebPlace[] = [
    { key: "brain" }, { key: "computer" }, { key: "sessions" }, { key: "session", workerId: "w 1/x" }, { key: "calendar" },
    { key: "extensions" }, { key: "screen" }, { key: "stats" }, { key: "resources" }, { key: "friends" }, { key: "settings" },
  ];

  test("every place round-trips through its path", () => {
    for (const place of places) expect(placeOfPath(placePath(place))).toEqual(place);
  });

  test("each place by its name; a Session under Sessions", () => {
    expect(placePath({ key: "brain" })).toBe("/");
    expect(placePath({ key: "sessions" })).toBe("/sessions");
    expect(placePath({ key: "extensions" })).toBe("/extensions");
    expect(placePath({ key: "calendar" })).toBe("/calendar");
    expect(placePath({ key: "session", workerId: "w1" })).toBe("/sessions/w1");
    expect(placePath({ key: "session", workerId: "a/b c" })).toBe("/sessions/a%2Fb%20c");
  });

  test("the computer page and the OAuth return have paths; odd ones land somewhere sensible", () => {
    expect(placeOfPath("/computer")).toEqual({ key: "computer" });
    expect(placePath({ key: "computer" })).toBe("/computer");
    expect(placeOfPath("/plugins")).toEqual({ key: "extensions" });
    expect(placeOfPath("/settings/")).toEqual({ key: "settings" });
    expect(placeOfPath("/onboarding")).toBeNull();
    expect(placeOfPath("/work")).toBeNull();
    expect(placeOfPath("")).toEqual({ key: "brain" });
  });

  test("only the paths something sends: not the Expo web app's /list, /terminal or /phone", () => {
    for (const path of ["/list", "/terminal/w9?serverId=s1", "/terminal", "/phone"]) expect(placeOfPath(path)).toBeNull();
  });
});

describe("sub-pages have URLs of their own (parity #47)", () => {
  const pages: WebPlace[] = [
    { key: "settings", page: { name: "settings-computer", id: "s 1" } },
    { key: "settings", page: { name: "settings-providers" } },
    { key: "settings", page: { name: "settings-telegram" } },
    { key: "settings", page: { name: "settings-license", id: "react" } },
    { key: "friends", page: { name: "friends-post", id: "p1" } },
    { key: "friends", page: { name: "friends-person", id: "u/2" } },
    { key: "friends", page: { name: "friends-drafts" } },
    { key: "extensions", page: { name: "extensions-service", id: "notion" } },
    { key: "extensions", page: { name: "extensions-tools", service: "notion", account: "a1" } },
    { key: "extensions", page: { name: "extensions-agent", scope: "codex" } },
    { key: "work", page: { name: "workFile", id: "release.md" } },
  ];

  test("each round-trips through its path", () => {
    for (const place of pages) expect(placeOfPath(placePath(place))).toEqual(place);
  });

  test("the Expo app's paths: settings/<page>?id=, friends/post/<id>, plugins/<service>/tools, work/<id>", () => {
    expect(placePath({ key: "settings", page: { name: "settings-computer", id: "s1" } })).toBe("/settings/computer?id=s1");
    expect(placeOfPath("/settings/model-providers")).toEqual({ key: "settings", page: { name: "settings-providers" } });
    expect(placeOfPath("/settings/computer")).toEqual({ key: "settings" });
    expect(placeOfPath("/plugins/notion/tools?account=a1")).toEqual({ key: "extensions", page: { name: "extensions-tools", service: "notion", account: "a1" } });
    expect(placeOfPath("/plugins/agents/claude-code")).toEqual({ key: "extensions", page: { name: "extensions-agent", scope: "claude-code" } });
    expect(placeOfPath("/work/w1")).toEqual({ key: "work", page: { name: "workFile", id: "w1" } });
    expect(placeOfPath("/friends/nowhere")).toEqual({ key: "friends" });
  });
});

describe("tab titles", () => {
  test("the place's name, or the page's own, then Mewla", () => {
    expect(documentTitle({ key: "calendar" })).toBe("Calendar · Mewla");
    expect(documentTitle({ key: "extensions" })).toBe("Extensions · Mewla");
    expect(documentTitle({ key: "session", workerId: "w1" })).toBe("Session · Mewla");
    expect(documentTitle({ key: "session", workerId: "w1" }, "  api server ")).toBe("api server · Mewla");
    expect(documentTitle(null)).toBe("Mewla");
  });
});

describe("sidebar width", () => {
  test("clamped to 216–400, the default without a saved number", () => {
    expect(storedSidebarWidth(null)).toBe(264);
    expect(storedSidebarWidth("")).toBe(264);
    expect(storedSidebarWidth("wide")).toBe(264);
    expect(storedSidebarWidth("300.4")).toBe(300);
    expect(storedSidebarWidth("80")).toBe(216);
    expect(storedSidebarWidth("9000")).toBe(400);
    expect(clampPanelWidth(Number.NaN, 216, 400)).toBe(216);
  });

  test("page columns per DESIGN.md", () => {
    expect(pageColumnWidth("page")).toBe(760);
    expect(pageColumnWidth("wide")).toBe(1200);
    expect(pageColumnWidth("reading")).toBe(820);
    expect(pageColumnWidth("full")).toBeNull();
  });
});
