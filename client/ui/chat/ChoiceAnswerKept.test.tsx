import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } };
const options = (...labels: string[]) => labels.map((label) => ({ label, description: `${label}, in a line` }));
// The owner's 11-24 card: two questions, each answered through Other with typed Chinese text.
const asked = {
  id: "claude-tool:toolu_1", seq: 1, timestamp: new Date().toISOString(), kind: "tool", tool_name: "AskUserQuestion", call_id: "toolu_1",
  choice: { kind: "AskUserQuestion", state: "pending", questions: [
    { header: "黄金方向", question: "黄金策略接下来往哪个方向改进？", options: options("换信号源（推荐）", "做稳健性检验", "先停在这里") },
    { header: "价值因子", question: "华泰价值因子接下来做什么？", options: options("本地重跑并补齐（推荐）", "扩展到改进型因子", "写成研报流程") },
  ] },
};

async function answerBoth(replies: Record<string, { type: string } & Record<string, unknown>>) {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": [asked] }, replies }) });
  await app.waitFor("id:request-card", { timeout: 10_000 });
  await app.wait(500);
  for (const text of ["把当前策略画成echarts框架的html给我看看", "先展示当前研究进展"]) {
    await app.press("id:choice-other-open");
    await app.type("id:choice-other", text);
    await app.press("id:choice-next");
  }
  await app.waitFor("id:choice-submit");
  await app.press("id:choice-submit");
  return app;
}

describe("a typed answer is never lost (owner 2026-11-24, Windows user's video)", () => {
  test("the question closed before the answer reached it: the answer waits in the message box, the line says Not sent, never Not answered", async () => {
    const app = await answerBoth({ answer_choice: { type: "error", code: "answer_choice_gone", message: "This question has already passed." } });
    await app.waitFor("id:request-sealed", { timeout: 10_000 });
    expect(framesOf(app.host.posts(), "answer_choice").at(-1)).toMatchObject({ worker_id: "brain-host",
      choice_answer: { call_id: "toolu_1", answers: [{ selected: [], other: "把当前策略画成echarts框架的html给我看看" }, { selected: [], other: "先展示当前研究进展" }] } });
    const message = "黄金方向: 把当前策略画成echarts框架的html给我看看\n价值因子: 先展示当前研究进展";
    await app.waitUntil(() => app.value("id:composer") === message, { timeout: 3_000 });
    expect(app.texts()).toContain("Not sent");
    expect(app.texts()).not.toContain("Not answered");
    expect(app.texts()).toContain("The question closed. Your answer is in the message box.");
    expect(app.texts()).not.toContain("This request has already passed.");
  }, 25_000);

  test("a refusal keeps the card and what was typed, with the daemon's words", async () => {
    const app = await answerBoth({ answer_choice: { type: "error", code: "answer_choice_refused", message: "The Terminal doesn't show this question; answer it there." } });
    await app.waitFor("id:request-failed", { timeout: 10_000 });
    expect(app.texts()).toContain("The Terminal doesn't show this question; answer it there.");
    expect(app.shows("id:request-card")).toBe(true);
    expect(app.texts()).toContain("把当前策略画成echarts框架的html给我看看");
    expect(app.enabled("id:choice-submit")).toBe(true);
  }, 25_000);
});
