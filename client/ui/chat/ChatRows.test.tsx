import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

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

describe("code in a reply", () => {
  test("a code block has Copy, and it copies the code", async () => {
    const app = await brainWith([reply("a1", 1, "Run:\n\n```sh\nmewla update --channel beta\n```\n")]);
    await app.waitFor("label:Copy", { timeout: 10_000 });
    await app.wait(300);
    await app.press("label:Copy");
    await app.waitUntil(() => (app.clipboard() ?? "").includes("mewla update --channel beta"));
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
