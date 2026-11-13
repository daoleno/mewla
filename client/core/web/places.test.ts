import { describe, expect, test } from "bun:test";
import {
  clampPaneWidth,
  documentTitle,
  isWide,
  nativeHeaderTitleWidth,
  pageColumnWidth,
  placeOfPath,
  placePath,
  showsListDetail,
  stepPaneWidth,
  storedPaneWidth,
  type WebPlace,
} from "./places";

describe("the wide gate (SPEC §14.6)", () => {
  test("two panes from a 1024 pt window, whatever the device", () => {
    expect(isWide(1024)).toBe(true);
    expect(isWide(1023)).toBe(false);
    expect(isWide(1366)).toBe(true);
  });

  test("list and detail side by side from an 880 pt right pane, on wide only", () => {
    expect(showsListDetail({ wide: true, paneWidth: 880 })).toBe(true);
    expect(showsListDetail({ wide: true, paneWidth: 879 })).toBe(false);
    // A phone held sideways has the room but not the two panes: the item pushes.
    expect(showsListDetail({ wide: false, paneWidth: 932 })).toBe(false);
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
    { key: "work", page: { name: "workDetail", id: "docsref" } },
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
    // A Work's detail (SPEC §14.5 #work-detail/docsref); the bare path is no place.
    expect(placePath({ key: "work", page: { name: "workDetail", id: "docsref" } })).toBe("/work-detail/docsref");
    expect(placeOfPath("/work-detail")).toBeNull();
    expect(placeOfPath("/brain/work/docsref")).toEqual({ key: "work", page: { name: "workDetail", id: "docsref" } });
    expect(placeOfPath("/friends/nowhere")).toEqual({ key: "friends" });
  });
});

describe("tab titles", () => {
  test("the place's name, or the page's own, then Mewla", () => {
    expect(documentTitle({ key: "calendar" })).toBe("Calendar · Mewla");
    expect(documentTitle({ key: "extensions" })).toBe("Extensions · Mewla");
    expect(documentTitle({ key: "sessions" })).toBe("All Sessions · Mewla");
    expect(documentTitle({ key: "computer" })).toBe("Computer · Mewla");
    expect(documentTitle({ key: "session", workerId: "w1" })).toBe("Session · Mewla");
    expect(documentTitle({ key: "session", workerId: "w1" }, "  api server ")).toBe("api server · Mewla");
    expect(documentTitle(null)).toBe("Mewla");
  });
});

describe("the left pane's width (SPEC §14.1)", () => {
  test("clamped to 280–420, 320 without a saved number", () => {
    expect(storedPaneWidth(null)).toBe(320);
    expect(storedPaneWidth("")).toBe(320);
    expect(storedPaneWidth("wide")).toBe(320);
    expect(storedPaneWidth("300.4")).toBe(300);
    expect(storedPaneWidth("80")).toBe(280);
    expect(storedPaneWidth("9000")).toBe(420);
    expect(clampPaneWidth(Number.NaN)).toBe(320);
  });

  test("← and → step 16 and stop at the ends", () => {
    expect(stepPaneWidth(320, 1)).toBe(336);
    expect(stepPaneWidth(320, -1)).toBe(304);
    expect(stepPaneWidth(412, 1)).toBe(420);
    expect(stepPaneWidth(290, -1)).toBe(280);
  });

  test("reading columns: list pages 680, threads 760, the rest the whole pane", () => {
    expect(pageColumnWidth("page")).toBe(680);
    expect(pageColumnWidth("reading")).toBe(760);
    expect(pageColumnWidth("full")).toBeNull();
  });
});

describe("a native header's own title view (owner 11-13: a long Session title pushed ⋯ off the screen)", () => {
  test("is as wide as the bar, not as its text: on Android 16 after the edge to 8 before the other", () => {
    expect(nativeHeaderTitleWidth({ stackWidth: 363, os: "android", systemBack: false })).toBe(339);
    expect(nativeHeaderTitleWidth({ stackWidth: 363.4, os: "android", systemBack: true })).toBe(339);
  });
  test("on iOS, after the system's Back when it shows", () => {
    expect(nativeHeaderTitleWidth({ stackWidth: 390, os: "ios", systemBack: false })).toBe(358);
    expect(nativeHeaderTitleWidth({ stackWidth: 390, os: "ios", systemBack: true })).toBe(302);
  });
});
