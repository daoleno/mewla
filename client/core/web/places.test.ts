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
