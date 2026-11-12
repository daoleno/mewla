import { describe, expect, open, test } from "nsh/test";
import { paired, settle } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";

function webHost(daemon: Record<string, unknown> = { brain: {} }) {
  const secure = new Map<string, string>();
  return {
    params: { daemon },
    handlers: {
      "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
      "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
      "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
      "app.legacyStorage": null,
      "app.legacyStorageCopied": null,
      "app.origin": "http://127.0.0.1:1",
      "links.initial": null,
      "app.testFrame": null,
      "web.initialPath": { path: "/", state: null },
      "web.setLocation": null,
    },
  };
}

const sessions = [
  { id: "w1", name: "api server", cwd: "/home/me/api", status: "running", started_at: 1, updated_at: 20 },
  { id: "w2", name: "docs", cwd: "/home/me/docs", status: "running", started_at: 2, updated_at: 10 },
];
const desk = (daemon?: Record<string, unknown>) => ({ platform: "web" as const, width: 1280, height: 800, storage: paired, host: webHost(daemon) });
/** The URL the app last gave the page: where it is (hidden sections stay mounted, so their pages count as shown). */
const path = (app: { host: { calls(): { name: string; data: unknown }[] } }) =>
  (app.host.calls().filter((call) => call.name === "web.setLocation").at(-1)?.data as { path?: string } | undefined)?.path;

describe("the command palette", () => {
  test("⌘K / Ctrl+K opens it on actions and places; a query finds Sessions and ranks; Return runs and closes it", async () => {
    const app = await open(APP, desk({ brain: {}, worker_sessions: sessions }));
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
    for (const label of ["Actions", "New session", "New chat", "Open Brain's terminal", "Browse workspace", "Switch executor", "Keyboard shortcuts", "Go to", "Calendar"]) expect(app.texts()).toContain(label);
    expect(app.texts()).toContain("G then C");
    await app.type("label:Search commands", "api");
    await app.waitFor("text:api server");
    expect(app.texts()).toContain("Sessions · /home/me/api");
    await app.clear("label:Search commands");
    await app.type("label:Search commands", "cal");
    // A query drops the headings and names each row's section instead. Wait for this query's rows: the last one's
    // (no headings either) can still be on screen.
    await app.waitFor("id:palette >> text:api server", { state: "gone" });
    await app.waitFor("text:Go to");
    expect(app.texts()).not.toContain("Actions");
    await app.keyboard.press("enter");
    await app.waitFor("id:palette", { state: "gone" });
    await app.waitUntil(() => path(app) === "/calendar");
  }, 20_000);

  test("a Session row opens that Session; nothing matching says so", async () => {
    const app = await open(APP, desk({ brain: {}, worker_sessions: sessions }));
    await app.waitFor("id:left-pane");
    await app.press("id:pane-search");
    await app.waitFor("id:palette");
    await app.type("label:Search commands", "zzz");
    await app.waitFor("id:palette-empty");
    await app.clear("label:Search commands");
    await app.type("label:Search commands", "docs");
    // Return runs the row the palette shows for this query: wait until it shows it alone.
    await app.waitFor("text:Sessions · /home/me/docs");
    await app.waitFor("id:palette >> text:api server", { state: "gone" });
    await app.keyboard.press("enter");
    await app.waitUntil(() => path(app) === "/sessions/w2");
  }, 20_000);

  test("the arrows wrap: ↑ from the first row is the last one", async () => {
    const app = await open(APP, desk());
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
    // It opens with the focus in its field (SPEC §14.3): the arrows are the palette's at once.
    await app.keyboard.press("up");
    await app.keyboard.press("enter");
    await app.waitFor("id:settings");
  }, 20_000);

  test("Escape closes it", async () => {
    const app = await open(APP, desk());
    await app.waitFor("id:left-pane");
    await settle(app);
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
    await app.keyboard.press("escape");
    await app.waitFor("id:palette", { state: "gone" });
  }, 20_000);
});

describe("the desktop's keys", () => {
  test("? lists every shortcut; Escape closes it", async () => {
    const app = await open(APP, desk());
    await app.waitFor("id:left-pane");
    await app.keyboard.press("shift+/");
    await app.waitFor("id:shortcuts");
    expect(app.texts()).toContain("Command palette");
    expect(app.texts()).toContain("Ctrl+K");
    expect(app.texts()).toContain("Edit your last message");
    expect(app.texts()).toContain("G then M");
    expect(app.texts()).toContain("G then X");
    await app.keyboard.press("escape");
    await app.waitFor("id:shortcuts", { state: "gone" });
  }, 20_000);

  // A manual clock: a step waits for the app to be quiet, which would otherwise run out G's 1.5 s.
  test("G then a letter goes to a place; N starts a new Session", async () => {
    const app = await open(APP, { ...desk(), clock: {} });
    await app.waitFor("id:left-pane");
    await app.keyboard.press("g");
    await app.keyboard.press("t");
    await app.waitFor("id:stats");
    await app.keyboard.press("g");
    await app.keyboard.press("c");
    await app.waitUntil(() => path(app) === "/calendar");
    await app.keyboard.press("n");
    await app.waitFor("id:new-cwd");
  }, 20_000);

  test("G waits 1.5 s for its letter, then the letter is only a letter", async () => {
    const app = await open(APP, { ...desk(), clock: {} });
    await app.waitFor("id:left-pane");
    await app.waitUntil(() => path(app) === "/");
    await app.keyboard.press("g");
    await app.clock!.runFor(1600);
    await app.keyboard.press("c");
    await app.wait(50);
    expect(path(app)).toBe("/");
  }, 20_000);

  test("a sheet is a dialog on the desktop, and Escape closes it", async () => {
    const app = await open(APP, desk());
    await app.waitFor("id:left-pane");
    await app.keyboard.press("n");
    await app.waitFor("id:new-session-sheet");
    await app.keyboard.press("escape");
    await app.waitFor("id:new-cwd", { state: "gone" });
  }, 20_000);

  test("the bare keys never fire while typing in a field", async () => {
    const app = await open(APP, desk());
    await app.waitFor("id:left-pane");
    await app.type("id:composer", "gn");
    await app.wait(100);
    expect(app.shows("id:new-cwd")).toBe(false);
    expect(app.value("id:composer")).toBe("gn");
  }, 20_000);

  test("keys work at any width with a hardware keyboard: below 1024 ⌘K opens the palette too", async () => {
    const app = await open(APP, { ...desk(), width: 800 });
    await app.waitFor("id:home-header");
    await settle(app);
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
  }, 20_000);

  test("G then M opens the computer page and G then X Screen; a palette pick replaces the page above Brain", async () => {
    const app = await open(APP, { ...desk({ brain: {}, worker_sessions: sessions }), clock: {} });
    await app.waitFor("id:left-pane");
    await app.keyboard.press("g");
    await app.keyboard.press("m");
    await app.waitUntil(() => path(app) === "/computer");
    await app.keyboard.press("g");
    await app.keyboard.press("x");
    await app.waitUntil(() => path(app) === "/screen");
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
    await app.type("label:Search commands", "docs");
    await app.waitFor("text:Sessions · /home/me/docs");
    await app.waitFor("id:palette >> text:api server", { state: "gone" });
    await app.keyboard.press("enter");
    await app.waitUntil(() => path(app) === "/sessions/w2");
    // Brain › that Session: one Back to Brain.
    await app.press("id:page-back");
    await app.waitUntil(() => path(app) === "/");
  }, 20_000);
});

describe("a terminal on screen (parity #27)", () => {
  test("with the focus outside the terminal, ⌘K and G then a letter still work", async () => {
    const app = await open(APP, desk({ brain: {}, worker_sessions: [{ ...sessions[0], command: "bash", provider: "" }] }));
    await app.waitFor("id:left-pane");
    await app.press("id:tray-all-sessions");
    await app.waitFor("id:session-row");
    await app.press("id:session-row");
    await app.waitFor("id:terminal-web", { timeout: 10_000 });
    await settle(app);
    await app.keyboard.press("mod+k");
    await app.waitFor("id:palette");
    await app.keyboard.press("escape");
    await app.waitFor("id:palette", { state: "gone" });
    await app.keyboard.press("g");
    await app.keyboard.press("c");
    await app.waitUntil(() => path(app) === "/calendar");
  }, 20_000);
});

