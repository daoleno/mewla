import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";
import { parentDirectoryPath } from "./NewSession";

const EXECUTORS = [
  { id: "claude", name: "claude", provider: "claude", command: "claude" },
  { id: "codex", name: "codex", provider: "codex", command: "codex" },
  { id: "agent", name: "agent", provider: "cursor", command: "cursor-agent --force --sandbox disabled" },
  { id: "agy", name: "agy", provider: "antigravity", command: "agy" },
];
const AGY = { agent: "Antigravity", install: "curl -fsSL https://antigravity.google/cli/install.sh | bash" };

const APP = "../FakeDaemonApp.tsx";

/** New session from the tray's +, on a window `height` high. */
async function newSession(height: number) {
  const app = await open(APP, { host: phoneHost({ brain: {} }), storage: paired, platform: "ios", width: 390, height });
  await app.host.send("links.open", { url: "mewla://sessions" });
  await app.waitFor("id:tray-new", { timeout: 10_000 });
  await app.press("id:tray-new");
  await app.waitFor("id:new-launch", { timeout: 10_000 });
  return app;
}

describe("New session fits the window (iPhone Duo, 11-14)", () => {
  for (const height of [844, 466, 300]) {
    test(`on a window ${height} high the Task field and Start are under the scroll, never in it`, async () => {
      // In no scroll, NSH keeps the focused field clear of the keyboard once the sheet rises; Start in the scroll sat
      // behind the keyboard on the iPhone Duo's cover screen held sideways (Mac re-run 11-14, 1c).
      const app = await newSession(height);
      expect(app.count("id:new-pinned >> id:new-launch")).toBe(1);
      expect(app.count("id:new-pinned >> id:new-task")).toBe(1);
      expect(app.count("id:new-scroll >> id:new-launch")).toBe(0);
      expect(app.count("id:new-scroll >> id:new-task")).toBe(0);
      // The agents still show on every window, down to 300 (the Task caption gives way below 500); when room runs
      // out, the scroll gives way first, never Task or Start.
      expect(app.count("id:new-scroll >> id:preset-claude")).toBe(1);
      // A short window gives the Task caption's room to the agents; the placeholder says it.
      expect(app.texts().includes("Task")).toBe(height >= 500);
    }, 30_000);
  }

  test("a custom command's two fields are under the scroll too, and Start starts it", async () => {
    const app = await newSession(844);
    await app.press("id:new-advanced");
    await app.waitFor("id:new-command");
    expect(app.count("id:new-pinned >> id:new-command")).toBe(1);
    expect(app.count("id:new-pinned >> id:new-name")).toBe(1);
    expect(app.count("id:new-scroll >> id:new-command")).toBe(0);
  }, 30_000);
});

describe("Up a folder", () => {
  test("on a Windows computer it goes up to the drive and stops there", () => {
    // Up did nothing on Windows: it only knew '/' (VM 11-16, the picker held at C:\Users\mtwo\cxproj).
    expect(parentDirectoryPath("C:\\Users\\mtwo\\cxproj")).toBe("C:\\Users\\mtwo");
    expect(parentDirectoryPath("C:\\Users\\mtwo\\")).toBe("C:\\Users");
    expect(parentDirectoryPath("C:\\Users")).toBe("C:\\");
    expect(parentDirectoryPath("C:\\")).toBe("C:\\");
    expect(parentDirectoryPath("D:/work/app")).toBe("D:/work");
    expect(parentDirectoryPath("D:/work")).toBe("D:/");
  });

  test("on Linux and macOS it goes up to /", () => {
    expect(parentDirectoryPath("/home/ana/app")).toBe("/home/ana");
    expect(parentDirectoryPath("/home")).toBe("/");
    expect(parentDirectoryPath("/")).toBe("/");
  });
});

/** New session on a computer that has some agents: Brain's snapshot lists the executors and which it found. */
async function newSessionOn(daemon: Record<string, unknown>) {
  const app = await open(APP, { host: phoneHost(daemon), storage: paired, platform: "ios", width: 390, height: 844 });
  await app.host.send("links.open", { url: "mewla://sessions" });
  await app.waitFor("id:tray-new", { timeout: 10_000 });
  await app.press("id:tray-new");
  await app.waitFor("id:new-launch", { timeout: 10_000 });
  return app;
}

const found = (...ids: string[]) => Object.fromEntries(ids.map((id) => [id, { installed: true }]));
const created = (app: Awaited<ReturnType<typeof newSessionOn>>) => framesOf(app.host.posts(), "create_session").length;

describe("New session on a computer with some agents missing", () => {
  test("a missing agent's tile is dimmed and says so, and the others are as before", async () => {
    const app = await newSessionOn({ brain: { executors: EXECUTORS, executor_states: found("claude", "codex", "agent") } });
    expect(app.shows("label:Antigravity, not installed")).toBe(true);
    expect(app.shows("label:Codex")).toBe(true);
    expect(app.shows("label:Shell")).toBe(true);
    expect(app.shows("label:Cursor, not installed")).toBe(false);
    // No command and no URL in the sheet: the dimmed tiles are the whole message.
    expect(app.texts().some((text) => /mewla deps|curl|https?:/.test(text))).toBe(false);
  }, 30_000);

  test("on a computer that has them all there is no mark", async () => {
    const app = await newSessionOn({ brain: { executors: EXECUTORS, executor_states: found("claude", "codex", "agent", "agy") } });
    expect(app.texts().some((text) => text.includes("not installed"))).toBe(false);
  }, 30_000);

  test("a computer that says nothing of its agents stops nothing", async () => {
    const app = await newSessionOn({ brain: {} });
    expect(app.shows("label:Antigravity")).toBe(true);
  }, 30_000);

  test("the last used agent is not here: the first that is, is chosen", async () => {
    // Claude (the default with no Session yet) is missing; Codex is the first agent that is here.
    const app = await newSessionOn({ brain: { executors: EXECUTORS, executor_states: found("codex") } });
    expect(app.shows("label:Codex, chosen")).toBe(true);
    expect(app.shows("label:Claude Code, not installed, chosen")).toBe(false);
  }, 30_000);

  test("Start on a missing agent is refused by the computer: one plain sentence in the sheet (no command, no URL), no Session is made, and another agent starts at once", async () => {
    const app = await newSessionOn({ brain: { executors: EXECUTORS, executor_states: found("claude") }, missing_agents: { agy: AGY } });
    await app.press("label:Antigravity, not installed");
    await app.press("id:new-launch");
    await app.waitFor("id:new-problem");
    expect(app.texts()).toContain("Antigravity is not installed on Desk.");
    expect(app.texts().some((text) => /curl|https?:|agy\)/.test(text))).toBe(false);
    expect(app.count("id:new-launch")).toBe(1);
    expect(created(app)).toBe(1);
    // The refusal is not an unknown outcome: no "Refresh required" wait.
    await app.press("label:Claude");
    await app.press("id:new-launch");
    await app.waitUntil(() => app.count("id:new-launch") === 0, { timeout: 10_000 });
    expect(created(app)).toBe(2);
  }, 30_000);

  test("a custom command stays free-form", async () => {
    const app = await newSessionOn({ brain: { executors: EXECUTORS, executor_states: found("claude") }, missing_agents: { agy: AGY } });
    await app.press("id:new-advanced");
    await app.waitFor("id:new-command");
    await app.type("id:new-command", "./my-tool --flag");
    await app.press("id:new-launch");
    await app.waitUntil(() => app.count("id:new-launch") === 0, { timeout: 10_000 });
    expect(created(app)).toBe(1);
  }, 30_000);
});
