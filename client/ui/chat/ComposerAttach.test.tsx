import { describe, expect, open, test } from "nsh/test";

const COMPOSER = "./testing/Composer.tsx";
const BASE = "http://desk.test:9877";
/** An invented picture: `onPick` hands its bytes as the photo the person picked. */
const PHOTO = { name: "Screenshot_20261124.png", type: "image/png", data: new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13]) };

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

/** The computer (`network`: what the app uploads itself) and the phone's host (`files.*`); `camera`: the host takes photos. */
function phone(options: { camera?: boolean } = {}) {
  const host: Record<string, (data: never) => unknown> = {
    ...secureStore(),
    "files.pick": () => [{ uri: "content://picker/plan.pdf", name: "plan.pdf", mimeType: "application/pdf", size: 2048 }],
    "files.upload": ({ fileUri }: { fileUri: string }) => ({ status: 200, body: JSON.stringify({ path: `/state/uploads/${fileUri.split("/").at(-1)}`, name: fileUri.split("/").at(-1) }) }),
    "files.cancel": () => null,
    "files.thumbnail": () => null,
  };
  if (options.camera) {
    host["files.canCapture"] = () => true;
    host["files.capture"] = () => ({ uri: "file:///cache/camera/shot.jpg", name: "shot.jpg", mimeType: "image/jpeg", size: 4096 });
  }
  return {
    host: { params: { state: "connected" }, handlers: host },
    network: { [`POST ${BASE}/upload`]: () => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/photo.png", name: "photo.png" }) }) },
  };
}

describe("the composer's +", () => {
  test("on a phone it opens the sheet: Photos and Files, Camera only when the host can take one", async () => {
    const app = await open(COMPOSER, { ...phone(), platform: "android" });
    await app.press("id:composer-plus");
    await app.waitFor("id:attach-sheet");
    expect(app.shows("id:attach-photos")).toBe(true);
    expect(app.shows("id:attach-files")).toBe(true);
    expect(app.shows("id:attach-camera")).toBe(false);
    // Nothing else lives behind "+": two tiles, a word each.
    expect(app.texts().filter((text) => text.trim())).toEqual(["Photos", "Files"]);

    const withCamera = await open(COMPOSER, { ...phone({ camera: true }), platform: "ios" });
    await withCamera.press("id:composer-plus");
    await withCamera.waitFor("id:attach-camera");
  });

  test("Photos asks the system photo picker for the room left, several at once, as JPEG-or-as-is; the photo is a chip", async () => {
    const app = await open(COMPOSER, { ...phone(), platform: "android" });
    app.onPick(() => [PHOTO]);
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitFor("text:Ready");
    expect(app.shows("id:attach-sheet")).toBe(false);
    const pick = app.picks()[0]!;
    expect(pick).toMatchObject({ kind: "images", multiple: true, limit: 8, format: "compatible" });
  });

  test("on iOS a tile opens its picker over the open sheet, and the sheet goes once the photo is picked", async () => {
    const app = await open(COMPOSER, { ...phone(), platform: "ios" });
    const sheetAtPick: boolean[] = [];
    app.onPick(() => {
      sheetAtPick.push(app.shows("id:attach-sheet"));
      return [PHOTO];
    });
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitFor("text:Ready");
    expect(sheetAtPick).toEqual([true]);
    await app.waitUntil(() => !app.shows("id:attach-sheet"));
  });

  test("a picker that fails or comes back empty lets the next one open: picking never sticks", async () => {
    const shape = phone({ camera: true });
    const app = await open(COMPOSER, {
      ...shape,
      host: { ...shape.host, handlers: { ...shape.host.handlers, "files.capture": () => Promise.reject(new Error("The camera could not open.")) } },
      platform: "ios",
    });
    let photos = 0;
    app.onPick(() => (photos += 1) === 1 ? null : [PHOTO]);
    await app.press("id:composer-plus");
    await app.press("id:attach-camera");
    await app.waitFor("text:The camera could not open.");
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitUntil(() => app.picks().length === 1);
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitFor("text:Ready");
    expect(app.picks()).toHaveLength(2);
  });

  test("a closed picker attaches nothing and says nothing", async () => {
    const app = await open(COMPOSER, { ...phone(), platform: "android" });
    app.onPick(() => null);
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitUntil(() => app.picks().length === 1);
    await app.wait(100);
    expect(app.count("id:attachment-upload-1")).toBe(0);
    expect(app.shows("id:notice")).toBe(false);
  });

  test("a photo, a camera shot and a file are three chips; one removed, Send carries the other two", async () => {
    const app = await open(COMPOSER, { ...phone({ camera: true }), platform: "android" });
    app.onPick(() => [PHOTO]);
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitFor("text:Ready");
    await app.press("id:composer-plus");
    await app.press("id:attach-camera");
    await app.waitUntil(() => app.texts().filter((text) => text === "Ready").length === 2);
    await app.press("id:composer-plus");
    await app.press("id:attach-files");
    await app.waitUntil(() => app.texts().filter((text) => text === "Ready").length === 3);
    await app.press("id:attachment-upload-2-remove");
    await app.waitUntil(() => app.texts().filter((text) => text === "Ready").length === 2);
    await app.type("id:composer", "look");
    await app.press("id:send");
    await app.waitUntil(() => app.texts().includes("look /state/uploads/photo.png,/state/uploads/plan.pdf"));
  });

  test("the room left bounds the pick, and a full composer says so in the upload's words", async () => {
    const app = await open(COMPOSER, { ...phone(), platform: "android" });
    app.onPick((pick) => Array.from({ length: pick.limit }, (_, i) => ({ ...PHOTO, name: `p${i}.png` })));
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitUntil(() => app.texts().filter((text) => text === "Ready").length === 8);
    await app.press("id:composer-plus");
    await app.press("id:attach-photos");
    await app.waitFor("text:Remove an attachment before selecting more files.");
    expect(app.picks()).toHaveLength(1);
  });

  test("a refused or broken camera is one line, in the host's words", async () => {
    const shape = phone({ camera: true });
    const app = await open(COMPOSER, {
      ...shape,
      host: { ...shape.host, handlers: { ...shape.host.handlers, "files.capture": () => Promise.reject(new Error("Camera permission denied.")) } },
      platform: "android",
    });
    await app.press("id:composer-plus");
    await app.press("id:attach-camera");
    await app.waitFor("text:Camera permission denied.");
  });

  test("on a desktop or the web + is the file dialog at once: no sheet", async () => {
    for (const platform of ["web", "linux"] as const) {
      const app = await open(COMPOSER, { ...phone(), platform });
      await app.press("id:composer-plus");
      await app.waitFor("text:Ready");
      expect(app.shows("id:attach-sheet")).toBe(false);
    }
  });
});
