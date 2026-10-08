import { describe, expect, test } from "bun:test";
import {
  DESKTOP_RETAINED_PAGES,
  desktopDocumentTitle,
  desktopReadableRoute,
  desktopScreenRetained,
  desktopShellExcluded,
  desktopSidebarKey,
  desktopSidebarNavigation,
  desktopSidebarPath,
  isDesktopTopLevelRoute,
  isDesktopWeb,
  isKeyboardOnlyWeb,
} from "./desktopWeb";
import { PRIMARY_DRAWER_DESTINATIONS, PRIMARY_DRAWER_SETTINGS } from "./primaryDrawerDestinations";

describe("desktop web gate", () => {
  test("only a browser at least 1024 wide", () => {
    expect(isDesktopWeb("web", 1024)).toBe(true);
    expect(isDesktopWeb("web", 1023)).toBe(false);
    expect(isDesktopWeb("ios", 1366)).toBe(false);
    expect(isDesktopWeb("android", 1280)).toBe(false);
  });
});

describe("route mapping", () => {
  test("every path selects the sidebar row that owns it", () => {
    expect(desktopSidebarKey("/")).toBe("brain");
    expect(desktopSidebarKey("/work/abc")).toBe("brain");
    expect(desktopSidebarKey("/list")).toBe("sessions");
    expect(desktopSidebarKey("/terminal/w2533")).toBe("sessions");
    expect(desktopSidebarKey("/plugins/github/tools")).toBe("plugins");
    expect(desktopSidebarKey("/model-profiles")).toBe("settings");
    expect(desktopSidebarKey("/settings?addServer=1")).toBe("settings");
    expect(desktopSidebarKey("/browser")).toBeNull();
  });

  test("each sidebar row's path maps back to the row", () => {
    for (const destination of [...PRIMARY_DRAWER_DESTINATIONS, PRIMARY_DRAWER_SETTINGS]) {
      expect(desktopSidebarKey(destination.pathname)).toBe(destination.key as never);
      expect(desktopSidebarPath(destination.key as never)).toBe(destination.pathname);
    }
    expect(desktopSidebarPath("brain")).toBe("/");
    expect(desktopSidebarPath("sessions")).toBe("/list");
  });

  test("tab titles", () => {
    expect(desktopDocumentTitle("/")).toBe("Brain · Mewla");
    expect(desktopDocumentTitle("/plugins/linear")).toBe("Plugins · Mewla");
    expect(desktopDocumentTitle("/terminal/w25", "atlas-notes")).toBe("atlas-notes · Mewla");
    expect(desktopDocumentTitle("/terminal/w25", "  ")).toBe("Session · Mewla");
    expect(desktopDocumentTitle("/nowhere")).toBe("Mewla");
  });

  test("pairing and the screenshot fixtures keep the full window", () => {
    expect(desktopShellExcluded("/onboarding")).toBe(true);
    expect(desktopShellExcluded("/screenshot-demo")).toBe(true);
    expect(desktopShellExcluded("/calendar")).toBe(false);
  });

  test("menu destinations draw no Back; sub-pages keep it", () => {
    for (const name of ["calendar", "index", "skills", "stats", "resources", "settings"]) {
      expect(isDesktopTopLevelRoute(name)).toBe(true);
    }
    for (const name of ["model-profiles", "[service]/index", "terminal/[id]", "work/[id]", "browser"]) {
      expect(isDesktopTopLevelRoute(name)).toBe(false);
    }
  });

  test("chats and the terminal lay out their own width", () => {
    expect(desktopReadableRoute("skills")).toBe(true);
    expect(desktopReadableRoute("(primary)")).toBe(false);
    expect(desktopReadableRoute("terminal/[id]")).toBe(false);
  });
});

describe("sidebar navigation", () => {
  test("pages always open as a new history entry", () => {
    expect(desktopSidebarNavigation(["(primary)", "calendar"], "calendar")).toBe("open");
    expect(desktopSidebarNavigation(["(primary)"], "skills")).toBe("open");
  });

  test("Brain and Sessions switch in place when showing, and open as a page elsewhere", () => {
    expect(desktopSidebarNavigation(["(primary)"], "sessions")).toBe("switch");
    expect(desktopSidebarNavigation(["(primary)", "calendar"], "brain")).toBe("open");
    expect(desktopSidebarNavigation(["(primary)", "terminal/[id]"], "sessions")).toBe("open");
  });

  test("only the newest copy of a page, and only recent pages or home, stay mounted", () => {
    const stack = ["(primary)", "calendar", "skills", "stats", "resources", "settings"];
    expect(desktopScreenRetained(stack, 0)).toBe(true);
    expect(desktopScreenRetained(stack, stack.length - 1)).toBe(true);
    expect(desktopScreenRetained(stack, stack.length - DESKTOP_RETAINED_PAGES)).toBe(true);
    expect(desktopScreenRetained(stack, stack.length - DESKTOP_RETAINED_PAGES - 1)).toBe(false);
    // Brain opened again: the older copy is a history entry only.
    const reopened = ["(primary)", "skills", "(primary)", "calendar", "skills"];
    expect(desktopScreenRetained(reopened, 0)).toBe(false);
    expect(desktopScreenRetained(reopened, 1)).toBe(false);
    expect(desktopScreenRetained(reopened, 2)).toBe(true);
    expect(desktopScreenRetained(reopened, 4)).toBe(true);
  });
});

describe("keyboard-only web", () => {
  test("only web with no touch pointer drops the phone key row", () => {
    expect(isKeyboardOnlyWeb("web", false)).toBe(true);
    expect(isKeyboardOnlyWeb("web", true)).toBe(false);
    expect(isKeyboardOnlyWeb("web", null)).toBe(false);
    expect(isKeyboardOnlyWeb("ios", false)).toBe(false);
    expect(isKeyboardOnlyWeb("android", false)).toBe(false);
  });
});
