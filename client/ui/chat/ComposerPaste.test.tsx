import { describe, expect, open, test } from "nsh/test";

const COMPOSER = "./testing/Composer.tsx";
const BASE = "http://desk.test:9877";
/** A picture to paste: `paste`'s paths are the app folder's (client/ui). */
const PICTURE = "assets/pets/p13/working.webp";

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const computer = (state: "connected" | "waiting" = "connected") => ({
  host: { params: { state }, handlers: secureStore() },
  network: { [`POST ${BASE}/upload`]: () => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/working.webp", name: "working.webp" }) }) },
});

describe("pasting into the composer", () => {
  test("a pasted picture becomes a chip above the field, and Send sends it with the text", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "web" });
    await app.paste("id:composer", { files: [PICTURE] });
    await app.waitFor("text:Ready");
    await app.waitFor("id:attachment-upload-1-thumbnail");
    await app.type("id:composer", "what is this?");
    await app.press("id:send");
    await app.waitUntil(() => app.texts().includes("what is this? /state/uploads/working.webp"));
  });

  test("with only a chip and no text, Send is on and sends the picture", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "web" });
    expect(app.enabled("id:send")).toBe(false);
    await app.paste("id:composer", { files: [PICTURE] });
    await app.waitFor("text:Ready");
    await app.waitUntil(() => app.enabled("id:send"));
    await app.press("id:send");
    await app.waitUntil(() => app.texts().includes(" /state/uploads/working.webp"));
  });

  test("a picture pasted from a phone keyboard becomes a chip too", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "android" });
    await app.paste("id:composer", { files: [PICTURE] });
    await app.waitFor("text:Ready");
  });

  test("pasted text goes into the field as typed, with no chip", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "web" });
    await app.paste("id:composer", { text: "npm run build" });
    await app.waitUntil(() => app.value("id:composer") === "npm run build");
    expect(app.shows("text:Ready")).toBe(false);
  });

  test("offline, a pasted picture says why it was not attached", async () => {
    const app = await open(COMPOSER, { ...computer("waiting"), platform: "web" });
    await app.paste("id:composer", { files: [PICTURE] });
    await app.waitFor("text:Connect to a computer to attach files.");
  });
});

describe("pasting into home's composer", () => {
  const APP = "../FakeDaemonApp.tsx";
  test("home: a pasted picture becomes a chip", async () => {
    const { paired, phoneHost } = await import("../testing/fixtures");
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain: { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude" } } }),
      network: { "POST http://127.0.0.1:1/upload": () => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/working.webp", name: "working.webp" }) }) } });
    await app.waitFor("text:What should Brain do?", { timeout: 10_000 });
    await app.wait(500);
    await app.paste("id:composer", { files: [PICTURE] });
    await app.waitFor("text:Ready", { timeout: 10_000 });
  }, 20_000);
});
