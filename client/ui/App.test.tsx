import { describe, expect, open, test, type App } from "nsh/test";
import theme from "./theme.json";

const secure = new Map<string, string>();
const host = {
  handlers: {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
    "app.legacyStorage": () => null,
    "app.legacyStorageCopied": () => undefined,
  },
};
const paired = {
  "mewla:v3:servers": JSON.stringify([
    { id: "s1", name: "Desk", url: "ws://127.0.0.1:1/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) },
  ]),
  "mewla:v1:current_server_id": "s1",
  "mewla:onboarded": "true",
};

describe("first run", () => {
  test("opens on the guided onboarding (Welcome); Screens/Onboarding.test.tsx walks it", async () => {
    const app = await open("./App.tsx", { host });
    await app.waitFor("id:set-up");
    expect(app.shows("id:onboarding")).toBe(true);
  });
});

describe("a paired phone", () => {
  test("the header cat plays its state's clip once, then holds the still; with less motion only the still", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android", clock: {} });
    // Connecting, then offline: it settles down (going_back) into the grey nap, and holds its still.
    await app.waitFor("text:Offline");
    expect(app.count("id:home-cat-sprite >> id:pet-still")).toBe(0);
    await app.clock!.runFor(6_000);
    await app.waitFor("id:home-cat-sprite >> id:pet-still");
    const still = await open("./App.tsx", { host, storage: paired, platform: "android", reducedMotion: true });
    await still.waitFor("id:home-cat-sprite >> id:pet-still");
    await still.waitFor("text:Offline");
    expect(still.count("id:home-cat-sprite >> id:pet-still")).toBe(1);
  });

  test("opens on home: the cat, Brain, the state control and ⋯ over the thread; no tab bar", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.waitFor("id:composer");
    for (const id of ["home-cat", "brain-menu"]) expect(app.count(`id:${id}`)).toBe(1);
    expect(app.texts()).toContain("Brain");
    expect(app.count("id:tabs")).toBe(0);
  });

  test("the cat opens the computer page: the computer and its connection, every rare place once, then Settings", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "ios" });
    await app.waitFor("id:home-cat");
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    expect(app.texts()).toContain("Desk");
    for (const id of ["screen", "extensions", "stats", "resources", "settings"]) expect(app.count(`id:computer-${id}`)).toBe(1);
    expect(app.count("id:pair-another")).toBe(1);
  });
});

describe("Brain", () => {
  test("says the computer is offline instead of spinning, and the composer is there", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.waitFor("id:brain-notice");
    await app.waitFor("text:Desk is offline", { timeout: 15_000 });
    expect(app.shows("id:composer")).toBe(true);
  }, 20_000);
});

describe("the tray", () => {
  test("offline, it says so with Try again and Details; New session is there", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("id:tray-notice", { timeout: 15_000 });
    expect(app.texts()).toContain("Try again");
    expect(app.texts()).toContain("Details");
    expect(app.shows("id:tray-new")).toBe(true);
  }, 20_000);
});

describe("the in-place upgrade from the Expo app", () => {
  test("opens on the computer the Expo app had, without pairing, and takes the entries only once", async () => {
    let offered = 0;
    let copied = false;
    const upgrade = {
      handlers: {
        ...host.handlers,
        "app.legacyStorage": () => (copied ? null : (offered++, { ...paired, "mewla.draft.v1:s1:chat": '{"t":1,"v":"half"}' })),
        "app.legacyStorageCopied": () => void (copied = true),
      },
    };
    const app = await open("./App.tsx", { host: upgrade, platform: "android" });
    await app.waitFor("id:composer");
    expect(app.shows("id:onboarding")).toBe(false);
    expect(copied).toBe(true);
    expect(app.storage()["mewla.draft.v1:s1:chat"]).toContain("half");
    const again = await open("./App.tsx", { host: upgrade, platform: "android" });
    await again.waitFor("id:onboarding");
    expect(offered).toBe(1);
  });
});

describe("Dark", () => {
  // NSH draws a Text with no color of its own black: in Dark the rows, headers and the composer's
  // words were black on the dark page (FINAL-VERIFY 6387f88f settings [94]).
  function blackTexts(app: App): string[] {
    const texts = app.texts().filter((text, i, all) => text.trim() !== "" && all.indexOf(text) === i);
    return texts.filter((text) => {
      try {
        return app.spanStyle(`text:${text}`).color === "#000000ff";
      } catch {
        return false; // a text that reads in more than one place, or inside a longer one
      }
    });
  }

  test("no text is drawn black on Brain, the computer page or Settings", async () => {
    const app = await open("./App.tsx", { host, storage: { ...paired, "mewla:theme_preference": "classic-dark" }, platform: "android", colorScheme: "light" });
    await app.waitFor("text:Desk is offline", { timeout: 15_000 });
    expect(blackTexts(app)).toEqual([]);
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    expect(blackTexts(app)).toEqual([]);
    await app.press("id:computer-settings");
    await app.waitFor("text:Dark");
    expect(blackTexts(app)).toEqual([]);
    // The theme's dark ink, from ui/theme.json.
    expect(app.spanStyle("text:Dark").color).toBe(`${theme.cssVars.dark.foreground.toLowerCase()}ff`);
  }, 25_000);
});
