import { describe, expect, open, test, type App } from "nsh/test";

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
  test("opens on the four sections, with the computer's connection in More", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.waitFor("id:composer");
    await app.host.send("links.open", { url: "mewla://more" });
    await app.waitFor("id:connection");
    expect(app.texts().join(" ")).toMatch(/Desk · (Connecting…|Offline)/);
  });

  test("More lists every destination once, and Settings lists the computers", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "ios" });
    await app.host.send("links.open", { url: "mewla://more" });
    await app.waitFor("id:more-Settings");
    for (const title of ["Screen", "Extensions", "Stats", "Resources", "Settings"]) expect(app.count(`id:more-${title}`)).toBe(1);
    await app.press("id:more-Settings");
    await app.waitFor("id:pair-another");
    expect(app.texts()).toContain("Desk");
    expect(app.texts()).toContain("Current");
  });
});

describe("Brain", () => {
  test("says the computer is offline instead of spinning, and the composer is there", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.waitFor("id:brain-notice");
    await app.waitFor("text:Your computer is offline", { timeout: 15_000 });
    expect(app.shows("id:composer")).toBe(true);
  }, 20_000);
});

describe("Sessions", () => {
  test("says the computer is offline with a way to retry, and New session waits for it", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.host.send("links.open", { url: "mewla://sessions" });
    await app.waitFor("id:sessions-empty");
    await app.waitFor("text:Server offline", { timeout: 15_000 });
    expect(app.texts()).toContain("Retry connection");
    expect(app.shows("id:new-session")).toBe(true);
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

  test("no text is drawn black on Brain, More or Settings", async () => {
    const app = await open("./App.tsx", { host, storage: { ...paired, "mewla:theme_preference": "classic-dark" }, platform: "android", colorScheme: "light" });
    await app.waitFor("text:Your computer is offline", { timeout: 15_000 });
    expect(blackTexts(app)).toEqual([]);
    await app.host.send("links.open", { url: "mewla://more" });
    await app.waitFor("id:more-Settings");
    expect(blackTexts(app)).toEqual([]);
    await app.press("id:more-Settings");
    await app.waitFor("id:pair-another");
    await app.waitFor("text:Dark");
    expect(blackTexts(app)).toEqual([]);
    expect(app.spanStyle("text:Dark").color).toBe("#f4f0eaff"); // the theme's dark ink
  }, 25_000);
});
