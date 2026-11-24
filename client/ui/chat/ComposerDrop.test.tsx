import { describe, expect, open, test } from "nsh/test";

const COMPOSER = "./testing/Composer.tsx";
const BASE = "http://desk.test:9877";
/** A real file on this machine, as Finder or a file manager drops it: a `file:` URL (the test's own URL is `asset:`). */
const local = (path: string) => `file://${new URL(path, import.meta.url).pathname}`;
const PICTURE = local("../assets/pets/p13/working.webp");
const FOLDER = local("../assets/pets/p13/");
/** A chip's name, drawn breakable (word joiners between its letters). */
const shows = (app: { texts(): string[] }, name: string) => app.texts().some((text) => text.replace(/\u2060/g, "") === name);

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

/** The computer takes each upload and names where it keeps it. */
function computer(state: "connected" | "waiting" = "connected", handlers: Record<string, (data: never) => unknown> = {}) {
  let n = 0;
  return {
    host: { params: { state }, handlers: { ...secureStore(), ...handlers } },
    network: { [`POST ${BASE}/upload`]: () => ({ status: 200, body: JSON.stringify({ path: `/state/uploads/${(n += 1)}.webp`, name: "working.webp" }) }) },
  };
}

describe("dropping files on a conversation (Mewla for Mac)", () => {
  // NSH 0.6.1 hands a drop over as the files' URLs alone, as the headless `dropFiles` does: the app reads them.
  test("a file dropped on the conversation becomes a chip under its own name, and Send sends it", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.dropFiles("id:composer", [PICTURE]);
    await app.waitFor("text:Ready");
    await app.waitUntil(() => shows(app, "working.webp"));
    await app.type("id:composer", "what is this?");
    await app.press("id:send");
    await app.waitUntil(() => app.texts().includes("what is this? /state/uploads/1.webp"));
  });

  test("several files dropped at once on the conversation above are several chips", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.dropFiles("id:sent", [PICTURE, PICTURE]);
    await app.waitUntil(() => app.count("text:Ready") === 2);
  });

  test("a folder is not attached, and says so", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.dropFiles("id:file-drop-chat", [FOLDER]);
    await app.waitFor("text:Folders can't be attached.");
    expect(app.shows("text:Ready")).toBe(false);
  });

  test("offline, a dropped file says why it was not attached", async () => {
    const app = await open(COMPOSER, { ...computer("waiting"), platform: "macos" });
    await app.dropFiles("id:composer", [PICTURE]);
    await app.waitFor("text:Connect to a computer to attach files.");
  });
});

describe("pasting files into the composer (Mewla for Mac)", () => {
  test("a file copied in Finder comes with its contents and is a chip under its name, Finder's name out of the field", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.type("id:composer", "look at ");
    await app.paste("id:composer", { text: "working.webp", files: ["assets/pets/p13/working.webp"], uris: [PICTURE] });
    await app.waitFor("text:Ready");
    await app.waitUntil(() => shows(app, "working.webp"));
    await app.waitUntil(() => app.value("id:composer") === "look at ");
    await app.press("id:send");
    await app.waitUntil(() => app.texts().includes("look at  /state/uploads/1.webp"));
  });

  test("a file copied in Finder that came without its contents (NSH 0.6.1) is read and is a chip", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.type("id:composer", "look at ");
    await app.paste("id:composer", { text: "working.webp", uris: [PICTURE] });
    await app.waitFor("text:Ready");
    await app.waitUntil(() => shows(app, "working.webp"));
    await app.waitUntil(() => app.value("id:composer") === "look at ");
  });

  test("a file copied in Finder and deleted before the paste is a chip that says so, its name not in the field", async () => {
    // Finder copies a file reference; once the file is gone the Mac cannot turn it into a path or read it.
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.type("id:composer", "look at ");
    await app.paste("id:composer", { text: "gone.png", uris: ["file:///.file/id=6571367.8601"] });
    await app.waitFor("text:Mewla can't read this file.");
    await app.waitUntil(() => shows(app, "File"));
    await app.waitUntil(() => app.value("id:composer") === "look at ");
  });

  test("a spreadsheet cell stays text: the picture of it is not attached", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "macos" });
    await app.paste("id:composer", { text: "Q3 revenue 4,210", html: "<table><tr><td>Q3 revenue 4,210</td></tr></table>", files: ["assets/pets/p13/working.webp"] });
    await app.waitUntil(() => app.value("id:composer") === "Q3 revenue 4,210");
    await app.wait(300);
    expect(app.shows("text:Ready")).toBe(false);
  });
});

describe("pasting files into the composer in a browser", () => {
  test("a file copied in a file manager, its bytes and its path both on the clipboard, is a chip", async () => {
    const app = await open(COMPOSER, { ...computer(), platform: "web" });
    await app.paste("id:composer", { text: "working.webp", files: ["assets/pets/p13/working.webp"], uris: [PICTURE] });
    await app.waitFor("text:Ready");
  });
});

describe("dropping files on a conversation in a browser", () => {
  /** The page host's side (web/host.js): the dropped file kept as a picked one, uploaded by the page. */
  const page = () => computer("connected", {
    "files.upload": () => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/dropped.png", name: "shot.png" }) }),
    "files.thumbnail": () => null,
  });

  test("files dragged over the conversation show where they go, and dropped become chips that send", async () => {
    const app = await open(COMPOSER, { ...page(), platform: "web" });
    expect(app.shows("text:Drop to attach")).toBe(false);
    await app.host.send("files.dragOver", { target: "file-drop-chat" });
    await app.waitFor("text:Drop to attach");
    await app.host.send("files.dragOver", { target: null });
    await app.waitFor("text:Drop to attach", { state: "gone" });
    await app.host.send("files.dropped", { target: "file-drop-chat", files: [{ uri: "web-file:1", name: "shot.png", mimeType: "image/png", size: 2048 }] });
    await app.waitFor("text:Ready");
    await app.waitUntil(() => shows(app, "shot.png"));
    await app.press("id:send");
    await app.waitUntil(() => app.texts().includes(" /state/uploads/dropped.png"));
    expect(app.host.calls().filter((call) => call.name === "files.upload").map((call) => (call.data as { fileUri: string }).fileUri)).toEqual(["web-file:1"]);
  });

  test("a drop on another page's place is not this one's", async () => {
    const app = await open(COMPOSER, { ...page(), platform: "web" });
    await app.host.send("files.dropped", { target: "file-drop-session-w1", files: [{ uri: "web-file:2", name: "shot.png", mimeType: "image/png", size: 2048 }] });
    await app.wait(300);
    expect(shows(app, "shot.png")).toBe(false);
  });

  test("more files than the composer holds are refused with the picker's words", async () => {
    const app = await open(COMPOSER, { ...page(), platform: "web" });
    const files = Array.from({ length: 11 }, (_, i) => ({ uri: `web-file:${i + 10}`, name: `shot ${i}.png`, mimeType: "image/png", size: 10 }));
    await app.host.send("files.dropped", { target: "file-drop-chat", files });
    await app.waitUntil(() => app.texts().some((text) => text.startsWith("Choose up to")));
  });
});
