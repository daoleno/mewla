import { describe, expect, test } from "bun:test";
import {
  clampPaneWidth,
  documentTitle,
  isWide,
  nextWide,
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
  test("two panes from 1024 pt of usable width, whatever the device and its shape", () => {
    expect(isWide({ width: 1024, height: 768 })).toBe(true);
    expect(isWide({ width: 1366, height: 1024 })).toBe(true);
    // A 13-inch tablet upright, and a short desktop window, as before.
    expect(isWide({ width: 1032, height: 1376 })).toBe(true);
    expect(isWide({ width: 1280, height: 400 })).toBe(true);
    expect(isWide({ width: 1023, height: 1366 })).toBe(false);
  });

  test("a window on its side from 760 pt and at least 480 high: an open foldable held like a book", () => {
    // iPhone Duo, inner screen on its side: 951 x 669 less the 84 pt side bar.
    expect(isWide({ width: 951 - 84, height: 669 })).toBe(true);
    // The Android SDK's 7.6" Fold-in on its side (1768 x 2208 px at 420 dpi: 841 x 674 dp) less a 32 dp cutout, a desktop window of 800 x 600.
    expect(isWide({ width: 841 - 32, height: 674 })).toBe(true);
    expect(isWide({ width: 800, height: 600 })).toBe(true);
    expect(isWide({ width: 760, height: 480 })).toBe(true);
    expect(isWide({ width: 759, height: 480 })).toBe(false);
    expect(isWide({ width: 900, height: 479 })).toBe(false);
  });

  test("a wide window keeps its pane while only its height shrinks (Android 14 resizes over the keyboard)", () => {
    const fold = { width: 809, height: 674 };
    const open = nextWide(null, fold);
    expect(open).toBe(true);
    // The keyboard comes up: 674 → 400 at the same width.
    const typing = nextWide({ wide: open, width: fold.width }, { width: 809, height: 400 });
    expect(typing).toBe(true);
    expect(nextWide({ wide: typing, width: 809 }, fold)).toBe(true);
    // A phone turned on its side starts short and never becomes wide this way.
    expect(nextWide({ wide: false, width: 390 }, { width: 835, height: 400 })).toBe(false);
    expect(nextWide({ wide: false, width: 835 }, { width: 835, height: 400 })).toBe(false);
    // A turn, a fold or a resize changes the width and decides afresh.
    expect(nextWide({ wide: true, width: 809 }, { width: 674, height: 400 })).toBe(false);
    expect(nextWide({ wide: true, width: 809 }, { width: 800, height: 400 })).toBe(false);
  });

  test("upright, folded or a phone held sideways: one stack", () => {
    // iPhone Duo: inner upright, the cover screen upright and on its side (less the side bar).
    expect(isWide({ width: 669, height: 951 })).toBe(false);
    expect(isWide({ width: 466 - 84, height: 678 })).toBe(false);
    expect(isWide({ width: 678 - 84, height: 466 })).toBe(false);
    // An 11-inch tablet upright reads like a phone (SPEC §10.3); a square fold upright too.
    expect(isWide({ width: 834, height: 1194 })).toBe(false);
    expect(isWide({ width: 800, height: 800 })).toBe(false);
    // A phone on its side: wide enough, too short.
    expect(isWide({ width: 932 - 118, height: 430 })).toBe(false);
    expect(isWide({ width: 915, height: 412 })).toBe(false);
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
