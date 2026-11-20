import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

// Owner 11-20: a wide table in Brain's reply was cut off on the right and would not scroll sideways. On Android
// with NSH 0.6.0 the chat list took every sideways drag that started inside it and cancelled the table's own
// scroll view once the thread was long enough to scroll (ScrollBox.claim; NSH 0.6.1 keeps the drag with the
// table, longbridge/nsh#2584). Headless runs no Android host, so this holds the app's side: a sideways drag
// on a table or a code block leaves the thread where it is, and a drag down that starts on a table still
// scrolls the thread. The Android host itself is proven with `nsh test --target android` on a device.
const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const reply = (id: string, seq: number, body: string) => ({ id, seq, timestamp: `2026-11-20T09:48:${String(seq).padStart(2, "0")}.000Z`, kind: "assistant_message", role: "assistant", body });
// From Brain's reply of 11-20 17:48: four columns of long Chinese cells, far wider than a phone.
const TABLE = [
  "| 问题 | 原因 | 修复 | PR |",
  "|---|---|---|---|",
  "| 浏览要 13–17 秒 | 每个文档的首段都要现算一次，统计项还要扫全部 128 万段 | 每个文档标一条\"首行\"，浏览和统计只读首行；本地同等规模下 7 秒 → 几十毫秒 | #136 |",
  "| 2028 年的那一行 | 文件名打错了日期 | 发布日期不得晚于文件到达时间，否则用文件夹的年月 | #137 |",
  "| 机构下拉只有 \"Any institution\" | 选项取自浏览统计，带关键词时就没有 | 检索台单独取一次完整机构列表 | #138 |",
].join("\n");
const CODE = "```sh\nGH_TOKEN=$(gh auth token -u daoleno) gh api \"repos/longbridge/nsh/commits?path=crates/nsh-platform-android/java/com/longbridge/nsh/ScrollBox.java&per_page=10\"\n```";
// Enough earlier replies that the thread scrolls: the case the list used to take the drag in.
const earlier = Array.from({ length: 12 }, (_, i) => reply(`f${i}`, i + 1, `Earlier reply ${i}: ${"检索台的质量修复已全部写成 PR。".repeat(12)}`));

async function brainWithTable() {
  const host = phoneHost({ brain, conversations: { "brain-host": [...earlier, reply("a1", 20, `The fixes:\n\n${TABLE}\n\nThe command:\n\n${CODE}\n\nEnd of reply.`)] } });
  const app = await open(APP, { platform: "android", width: 390, height: 844, storage: paired, host });
  await app.waitFor("id:composer", { timeout: 10_000 });
  await app.waitFor("text:End of reply.", { timeout: 10_000 });
  await app.wait(300);
  return app;
}

/** What the thread's list did since `from` posts: its drags and scrolls (`core/timeline/scrollTrace.ts`). */
const threadMoves = (app: Awaited<ReturnType<typeof brainWithTable>>, from: number) => app.host.posts().slice(from)
  .filter((post) => post.name === "app.scrollTrace")
  .map((post) => post.data as { kind: string })
  .filter((entry) => entry.kind === "drag" || entry.kind === "scroll");

describe("a wide table or code block in a reply scrolls in its own box", () => {
  test("a sideways drag on a table's cell or a code block leaves the thread where it is", async () => {
    const app = await brainWithTable();
    const from = app.host.posts().length;
    await app.drag("text:浏览要 13–17 秒", { x: -300, y: 0 });
    await app.drag("text:2028 年的那一行", { x: 300, y: 0 });
    await app.drag("id:code-block", { x: -300, y: 0 });
    expect(threadMoves(app, from)).toEqual([]);
  }, 30_000);

  test("a drag down that starts on a table scrolls the thread", async () => {
    const app = await brainWithTable();
    const from = app.host.posts().length;
    await app.drag("text:2028 年的那一行", { x: 0, y: 300 });
    const moves = threadMoves(app, from) as { kind: string; phase?: string }[];
    expect(moves[0]).toEqual({ ...moves[0], kind: "drag", phase: "start" });
    expect(moves.some((entry) => entry.kind === "scroll")).toBe(true);
  }, 30_000);
});
