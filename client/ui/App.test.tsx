import { describe, expect, open, test } from "nsh/test";

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
  test("opens on pairing and explains a pasted text that is not a pairing link", async () => {
    const app = await open("./App.tsx", { host });
    expect(app.shows("id:scan")).toBe(true);
    await app.type("id:link", "hello");
    await app.press("id:pair");
    await app.waitFor("id:failure");
    expect(app.texts().join(" ")).toContain("isn't a Mewla pairing code");
  });
});

describe("a paired phone", () => {
  test("opens on the four sections, with the computer's connection in More", async () => {
    const app = await open("./App.tsx", { host, storage: paired, platform: "android" });
    await app.waitFor("text:Brain");
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
    await app.waitFor("text:Brain");
    expect(app.shows("id:scan")).toBe(false);
    expect(copied).toBe(true);
    expect(app.storage()["mewla.draft.v1:s1:chat"]).toContain("half");
    const again = await open("./App.tsx", { host: upgrade, platform: "android" });
    await again.waitFor("id:scan");
    expect(offered).toBe(1);
  });
});
