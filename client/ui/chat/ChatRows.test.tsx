import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";
import { MONO_FAMILY } from "../kit/monoFont";
import theme from "../theme.json";

const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const at = (s: number) => `2026-11-12T07:00:${String(s).padStart(2, "0")}.000Z`;
const reply = (id: string, seq: number, body: string) => ({ id, seq, timestamp: at(seq), kind: "assistant_message", role: "assistant", body });

async function brainWith(events: Record<string, unknown>[], daemon: Record<string, unknown> = {}) {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": events }, ...daemon }) });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

describe("links in Brain's reply", () => {
  test("a web link opens in the browser", async () => {
    const app = await brainWith([reply("a1", 1, "Read [the release notes](https://example.com/notes) first.")]);
    await app.waitFor("text:the release notes", { timeout: 10_000 });
    await app.wait(300);
    await app.press("text:the release notes");
    await app.waitUntil(() => app.openedUrls().includes("https://example.com/notes"));
  }, 20_000);

  test("a file path opens Brain's file in the preview, and Back returns to the thread", async () => {
    const app = await brainWith([reply("a1", 1, "I wrote [the plan](/home/me/worklog/plan.md).")]);
    await app.waitFor("text:the plan", { timeout: 10_000 });
    await app.wait(300);
    await app.press("text:the plan");
    await app.waitFor("id:file-preview", { timeout: 10_000 });
    expect(app.openedUrls()).toEqual([]);
    await app.back();
    await app.waitFor("id:home-header");
  }, 20_000);
});

describe("files Brain names in a reply (owner 11-15)", () => {
  const PDF = "/home/me/worklog/0g-pay/0G-Pay-Unifra-Plan-zh.pdf";
  const asked = (app: Awaited<ReturnType<typeof brainWith>>) => framesOf(app.host.posts(), "session_file_metadata");

  test("a path in inline code opens in the preview, read through Brain's own Session", async () => {
    // Brain's host start comes as the daemon writes a time (RFC 3339, nanoseconds); the file routes take milliseconds.
    const host = { ...host_worker, started_at: "2025-11-11T06:00:00.123456789Z" };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain: { ...brain, host_worker: host }, conversations: { "brain-host": [reply("a1", 1, `计划书：\`${PDF}\``)] } }) });
    // `text:` reads a leading "/" as a pattern: the link's own text, matched whole.
    await app.waitFor("text:/^\\/home\\/me\\/worklog\\/0g-pay\\/0G-Pay-Unifra-Plan-zh\\.pdf$/", { timeout: 10_000 });
    await app.wait(300);
    await app.press("text:/^\\/home\\/me\\/worklog\\/0g-pay\\/0G-Pay-Unifra-Plan-zh\\.pdf$/");
    await app.waitFor("id:file-preview", { timeout: 10_000 });
    await app.waitUntil(() => asked(app).length > 0);
    expect(asked(app)[0]).toMatchObject({ worker_id: "brain-host", process_id: 4242, started_at: Date.UTC(2025, 10, 11, 6, 0, 0, 123), path: PDF });
    expect(app.openedUrls()).toEqual([]);
  }, 20_000);

  test("Brain's host start written in the computer's own zone (+08:00) is the same milliseconds", async () => {
    const host = { ...host_worker, started_at: "2025-11-11T14:00:00.987654321+08:00" };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain: { ...brain, host_worker: host }, conversations: { "brain-host": [reply("a1", 1, "见 /home/me/out/a.md")] } }) });
    await app.waitFor("text:/^\\/home\\/me\\/out\\/a\\.md$/", { timeout: 10_000 });
    await app.wait(300);
    await app.press("text:/^\\/home\\/me\\/out\\/a\\.md$/");
    await app.waitUntil(() => asked(app).length > 0);
    expect(asked(app)[0]).toMatchObject({ started_at: Date.UTC(2025, 10, 11, 6, 0, 0, 987), path: "/home/me/out/a.md" });
  }, 20_000);

  test("a path in the text opens too; a code block's path stays code", async () => {
    const app = await brainWith([reply("a1", 1, "图在/home/me/out/shot.png。\n\n```sh\nopen /home/me/out/other.png\n```\n")]);
    await app.waitFor("text:/^\\/home\\/me\\/out\\/shot\\.png$/", { timeout: 10_000 });
    await app.wait(300);
    await app.press("text:/^\\/home\\/me\\/out\\/shot\\.png$/");
    await app.waitFor("id:file-preview", { timeout: 10_000 });
    await app.waitUntil(() => asked(app).length > 0);
    expect(asked(app)[0]).toMatchObject({ path: "/home/me/out/shot.png" });
  }, 20_000);
});

describe("a path relative to Brain's workspace (owner 11-16: Brain's DESIGN-v2.md could not be opened)", () => {
  const DESIGN = "worklog/freeride-data-search-1115/DESIGN-v2.md";
  const text = `I rewrote it as v2 (\`${DESIGN}\`), and \`client/core/transport.ts\` is not here.`;
  const exists = (existing: string[]) => ({ replies: { session_file_exists: { type: "session_file_exists", existing } } });

  test("asked of Brain's own Session, a file there links and opens in the preview", async () => {
    const app = await brainWith([reply("a1", 1, text)], exists([DESIGN]));
    await app.waitUntil(() => framesOf(app.host.posts(), "session_file_exists").length > 0, { timeout: 10_000 });
    expect(framesOf(app.host.posts(), "session_file_exists")[0]).toMatchObject({ worker_id: "brain-host", process_id: 4242, started_at: 1_762_840_800_000, paths: [DESIGN, "client/core/transport.ts"] });
    await app.wait(300);
    await app.press(`text:${DESIGN}`);
    await app.waitFor("id:file-preview", { timeout: 10_000 });
    await app.waitUntil(() => framesOf(app.host.posts(), "session_file_metadata").length > 0);
    expect(framesOf(app.host.posts(), "session_file_metadata")[0]).toMatchObject({ worker_id: "brain-host", path: DESIGN });
  }, 20_000);

  test("a reply drawn before Brain's host run is known asks once it is", async () => {
    const early = { ...host_worker, process_id: 0 };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain: { ...brain, host_worker: early }, conversations: { "brain-host": [reply("a1", 1, text)] }, ...exists([DESIGN]) }) });
    await app.waitFor("id:reply", { timeout: 10_000 });
    await app.wait(300);
    expect(framesOf(app.host.posts(), "session_file_exists")).toEqual([]);
    await app.host.send("app.daemonPush", { type: "brain_snapshot", brain });
    await app.waitUntil(() => framesOf(app.host.posts(), "session_file_exists").length > 0, { timeout: 10_000 });
    expect(framesOf(app.host.posts(), "session_file_exists")[0]).toMatchObject({ worker_id: "brain-host", process_id: 4242 });
  }, 20_000);

  test("a path that is no file there stays text: no dead link", async () => {
    const app = await brainWith([reply("a1", 1, text)], exists([]));
    await app.waitUntil(() => framesOf(app.host.posts(), "session_file_exists").length > 0, { timeout: 10_000 });
    await app.wait(300);
    await app.press(`text:${DESIGN}`).catch(() => {});
    await app.wait(300);
    expect(app.shows("id:file-preview")).toBe(false);
    expect(framesOf(app.host.posts(), "session_file_metadata")).toEqual([]);
  }, 20_000);
});

describe("code in a reply", () => {
  test("a code block has a header with its language and Copy, which copies the code and says so", async () => {
    const app = await brainWith([reply("a1", 1, "Run:\n\n```sh\nmewla update --channel beta\n```\n")]);
    await app.waitFor("id:code-block", { timeout: 10_000 });
    expect(app.texts()).toContain("sh");
    await app.wait(300);
    await app.press("label:Copy");
    await app.waitUntil(() => (app.clipboard() ?? "").includes("mewla update --channel beta"));
    await app.waitFor("label:Copied");
  }, 20_000);
});

describe("a reply", () => {
  test("is its text alone: no Copy row under it, and no sheet of ours on a long press (the system selects it)", async () => {
    const app = await brainWith([reply("a1", 1, "Tonight at 23:30, and it is on the Calendar.")]);
    await app.waitFor("text:Tonight at 23:30, and it is on the Calendar.", { timeout: 10_000 });
    expect(app.shows("label:Copy")).toBe(false);
    // Nothing of ours hears a long press on it: the text is selectable, and the platform's selection takes it
    // (the headless host draws no selection; not provable here).
    let refused = "";
    try {
      await app.longPress("id:reply");
    } catch (error) {
      refused = String((error as Error).message ?? error);
    }
    expect(refused).toContain("no pressable view");
    expect(app.dialogs()).toEqual([]);
  }, 20_000);
});

describe("inline code", () => {
  test("reads at the body's optical size in the mono face, on the surface chip", async () => {
    const app = await brainWith([reply("a1", 1, "Run `bun test core` first.\n\n- item `zq-b`\n\n# Head `x`")]);
    await app.waitFor("text:Run bun test core first.", { timeout: 10_000 });
    // 15 in the 17 body (mono's x-height meets the body's), on the surface chip; the same in a list item.
    expect(app.spanStyle("text:Run bun test core first.").fontSize).toBe(17);
    expect(app.spanStyle("text:bun test core")).toMatchObject({ fontSize: 15, backgroundColor: "#ffffffff", color: "#131416ff" });
    expect(app.spanStyle("text:zq-b")).toMatchObject({ fontSize: 15, backgroundColor: "#ffffffff" });
  }, 20_000);

  test("is set in the face the app ships, not what a phone's vendor maps `monospace` to (a sans on the owner's OnePlus)", () => {
    // The theme's font-mono names the bundled family first (the system's after it, for what the face lacks, such as CJK).
    expect(MONO_FAMILY).toBe("Maple Mono NF");
    expect(theme.cssVars.theme["font-mono"].split(",")[0].trim()).toBe(`'${MONO_FAMILY}'`);
  });
});

describe("a long message the person sent folds", () => {
  const long = Array.from({ length: 30 }, (_, i) => `Line ${i + 1} of the pasted brief`).join("\n");
  const sent = (id: string, seq: number, body: string) => ({ id, seq, timestamp: at(seq), kind: "user_message", role: "user", body });

  test("past 8 lines it shows its first lines and a chevron; a short one never folds", async () => {
    const app = await brainWith([sent("u1", 1, long), sent("u2", 2, "Short\nand\nsweet"), reply("a1", 3, "Read it.")]);
    await app.waitFor("id:user-message-folded", { timeout: 10_000 });
    expect(app.count("id:user-message-fold")).toBe(1);
    expect(app.count("id:user-message-folded")).toBe(1);
    expect(app.count("id:user-message-body")).toBe(1);
    // The text under the fold is the whole message: Select all copies all of it, not the lines that show.
    expect(app.texts()).toContain(long);
  }, 20_000);

  test("the chevron opens and closes it, and it stays as left while the thread lives", async () => {
    const app = await brainWith([sent("u1", 1, long), reply("a1", 2, "Read it.")]);
    await app.waitFor("id:user-message-folded", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:user-message-fold");
    await app.waitFor("label:Fold the message");
    expect(app.shows("id:user-message-folded")).toBe(false);
    // The chevron is its own target: no edit.
    expect(app.shows("id:edit-chip")).toBe(false);
    await app.host.send("app.conversation", { worker: "brain-host", upserts: [reply("a2", 3, "And one more thing.")] });
    await app.waitFor("text:And one more thing.");
    expect(app.shows("label:Fold the message")).toBe(true);
    await app.press("id:user-message-fold");
    await app.waitFor("id:user-message-folded");
  }, 20_000);
});

describe("the person's own message", () => {
  test("a tap puts it in the composer to edit (the Editing chip; × leaves it), with no sheet", async () => {
    const app = await brainWith([{ id: "u1", seq: 1, timestamp: at(1), kind: "user_message", role: "user", body: "Book the 23:30 train" }, reply("a1", 2, "Booked.")]);
    await app.waitFor("text:Book the 23:30 train", { timeout: 10_000 });
    await app.wait(300);
    // A tap on its words (on the phone the text takes every touch on it).
    await app.press("id:user-message-text");
    await app.waitFor("id:edit-chip");
    expect(app.value("id:composer")).toBe("Book the 23:30 train");
    expect(app.dialogs()).toEqual([]);
    await app.press("id:edit-cancel");
    await app.waitFor("id:edit-chip", { state: "gone" });
    expect(app.value("id:composer")).toBe("");
  }, 20_000);
});

describe("edited files", () => {
  const patch = {
    id: "p1", seq: 2, timestamp: at(2), kind: "patch", role: "assistant", status: "completed",
    file_changes: [
      { path: "src/sync.ts", operation: "update", additions: 12, deletions: 3 },
      { path: "docs/old.md", move_path: "docs/new.md", operation: "update", additions: 1, deletions: 1 },
    ],
  };

  test("each edited file shows its +/−, and a renamed file shows and opens its new path", async () => {
    const app = await brainWith([reply("a0", 1, "Fixing it."), patch, reply("a2", 3, "Done.")]);
    await app.waitFor("id:activity", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:activity");
    await app.waitFor("id:activity-file");
    expect(app.texts()).toContain("src/sync.ts");
    expect(app.texts()).toContain("+12");
    expect(app.texts()).toContain("−3");
    expect(app.texts()).toContain("docs/new.md");
    expect(app.texts()).not.toContain("docs/old.md");
    await app.press("label:Open docs/new.md, 1 added, 1 removed");
    await app.waitFor("id:file-preview", { timeout: 10_000 });
    expect(app.texts()).toContain("docs/new.md");
  }, 20_000);

  test("a Claude Edit shows its +/− on the row, and its file from the project down, not the whole absolute path", async () => {
    const edit = {
      id: "e1", seq: 2, timestamp: at(2), kind: "tool", tool_name: "Edit", title: "Tool", status: "done", source: "claude_transcript",
      input: JSON.stringify({ file_path: "/home/someone/work/mewla/daemon/work/notes.md", old_string: "a", new_string: "b" }),
      files: ["/home/someone/work/mewla/daemon/work/notes.md"],
      file_changes: [{ path: "/home/someone/work/mewla/daemon/work/notes.md", operation: "update", additions: 2, deletions: 1 }],
    };
    const app = await brainWith([reply("a0", 1, "Fixing it."), edit, reply("a2", 3, "Done.")]);
    await app.waitFor("id:activity", { timeout: 10_000 });
    expect(app.texts().some((text) => text.includes("+2 −1"))).toBe(true);
    await app.wait(300);
    await app.press("id:activity");
    await app.waitFor("id:activity-file");
    expect(app.texts()).toContain("daemon/work/notes.md");
    expect(app.texts()).not.toContain("/home/someone/work/mewla/daemon/work/notes.md");
    expect(app.shows("label:Open /home/someone/work/mewla/daemon/work/notes.md, 2 added, 1 removed")).toBe(true);
  }, 20_000);
});

describe("a delegated Worker's Session", () => {
  const worker = { id: "w1", name: "Hello writer", status: "running", cwd: "/home/me/proj", command: "claude", provider: "claude", delegated: true, started_at: 1_762_840_800_000, process_id: 77, updated_at: Date.now() };
  const protocol = "Mewla lifecycle protocol:\nProfile: quick.\nYou are the delegated Worker: execute the assigned work directly.";
  const task = "Create the file hello.txt in the current directory containing exactly one line: hello. Overwrite it if it exists. Touch nothing else.\n\nReport back with the output of: cat -A hello.txt";
  const brief = (id: string, body: string, origin: string) => ({ id, seq: 1, timestamp: at(1), kind: "user_message", role: "user", body, origin });

  async function sessionWith(first: Record<string, unknown>) {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [worker], conversations: { w1: [first, reply("a1", 2, "Done: hello.txt holds one line.")] } }) });
    await app.waitFor("id:composer", { timeout: 10_000 });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("id:delegated-brief", { timeout: 10_000 });
    return app;
  }

  test("opens with the task in two lines, not the protocol and not as the person's bubble; a tap shows the whole task", async () => {
    const app = await sessionWith(brief("b1", `Execute:\n\n${task}\n\n${protocol}`, "delegated"));
    const shown = () => app.texts().join("\n");
    expect(shown()).not.toContain("Mewla lifecycle protocol");
    expect(shown()).not.toContain("Execute:");
    expect(app.shows("id:user-message")).toBe(false);
    expect(app.texts()).toContain(task);
    await app.wait(300);
    expect(app.shows("id:delegated-task")).toBe(true);
    await app.press("id:delegated-brief");
    await app.waitFor("id:delegated-task-open");
    expect(shown()).not.toContain("Mewla lifecycle protocol");
  }, 20_000);
});

describe("the day caption", () => {
  test("a thread opens with its day's caption, even when its first message came back without a time", async () => {
    const now = new Date().toISOString();
    const app = await brainWith([
      { id: "u1", seq: 1, kind: "user_message", role: "user", body: "Run the bash command touch /tmp/proof then say done" },
      { id: "a1", seq: 2, timestamp: now, kind: "assistant_message", role: "assistant", body: "Done. I created the empty file." },
    ]);
    await app.waitFor("id:date-divider", { timeout: 10_000 });
    const texts = app.texts();
    expect(texts.indexOf("Today")).toBeGreaterThanOrEqual(0);
    expect(texts.indexOf("Today")).toBeLessThan(texts.indexOf("Run the bash command touch /tmp/proof then say done"));
  }, 20_000);
});
