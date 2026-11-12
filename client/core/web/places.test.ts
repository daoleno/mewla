import { describe, expect, test } from "bun:test";
import {
  clampPanelWidth,
  documentTitle,
  isDesktopLayout,
  pageColumnWidth,
  placeOfLink,
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

  test("a Session selects Sessions and Work selects Brain", () => {
    expect(sidebarKeyOf({ key: "session", workerId: "w1" })).toBe("sessions");
    expect(sidebarKeyOf({ key: "work" })).toBe("brain");
    expect(sidebarKeyOf({ key: "calendar" })).toBe("calendar");
  });
});

describe("paths", () => {
  const places: WebPlace[] = [
    { key: "brain" }, { key: "work" }, { key: "sessions" }, { key: "session", workerId: "w 1/x" }, { key: "calendar" },
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

  test("the Expo web app's paths and odd ones still land somewhere sensible", () => {
    expect(placeOfPath("/list")).toEqual({ key: "sessions" });
    expect(placeOfPath("/plugins")).toEqual({ key: "extensions" });
    expect(placeOfPath("/terminal/w9?serverId=s1")).toEqual({ key: "session", workerId: "w9" });
    expect(placeOfPath("/terminal?id=w8")).toEqual({ key: "session", workerId: "w8" });
    expect(placeOfPath("/terminal")).toEqual({ key: "sessions" });
    expect(placeOfPath("/phone")).toEqual({ key: "screen" });
    expect(placeOfPath("/settings/")).toEqual({ key: "settings" });
    expect(placeOfPath("/onboarding")).toBeNull();
    expect(placeOfPath("")).toEqual({ key: "brain" });
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

describe("mewla:// links no other handler takes (parity #48)", () => {
  test("stats, resources, screen, a terminal, a Work file, a Friends post, a Plugins service", () => {
    expect(placeOfLink("mewla://stats")).toEqual({ key: "stats" });
    expect(placeOfLink("mewla://resources/")).toEqual({ key: "resources" });
    expect(placeOfLink("mewla://screen")).toEqual({ key: "screen" });
    expect(placeOfLink("mewla://terminal/w9?serverId=s1")).toEqual({ key: "session", workerId: "w9" });
    expect(placeOfLink("mewla://work/w1")).toEqual({ key: "work", page: { name: "workFile", id: "w1" } });
    expect(placeOfLink("mewla://friends/post/p1")).toEqual({ key: "friends", page: { name: "friends-post", id: "p1" } });
    expect(placeOfLink("mewla://plugins/notion")).toEqual({ key: "extensions", page: { name: "extensions-service", id: "notion" } });
  });

  test("the links other handlers take stay theirs", () => {
    for (const url of ["mewla://brain", "mewla://sessions/w1", "mewla://calendar?id=c1", "mewla://live/brain", "mewla://settings/computer?id=s1", "mewla://phone", "mewla://plugins", "mewla://plugins?state=x", "mewla://more", "https://x.test/stats"]) {
      expect(placeOfLink(url)).toBeNull();
    }
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
