import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

// Owner 11-16: 「flowchart 在 mewla 里也要支持一下」. Pele draws here as on the phone (no DOM); each drawing is posted as `app.mermaidRender`.
const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const at = (s: number) => `2026-11-16T07:00:${String(s).padStart(2, "0")}.000Z`;
const reply = (id: string, seq: number, body: string, extra: Record<string, unknown> = {}) => ({ id, seq, timestamp: at(seq), kind: "assistant_message", role: "assistant", body, ...extra });
const FLOW = 'flowchart LR\n  subgraph DP["data-platform（权威）"]\n    SRC["源 Space<br/>OpenList"]\n  end\n  SRC -- "API 读源 Version" --> W["derive-worker"]';
const fence = (source: string) => `The design:\n\n\`\`\`mermaid\n${source}\n\`\`\`\n\nThat is all.`;

async function brainWith(events: Record<string, unknown>[]) {
  const host = phoneHost({ brain, conversations: { "brain-host": events } }, RECORD);
  const app = await open(APP, { platform: "android", width: 390, height: 844, storage: paired, host });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

const RECORD = { "app.mermaidRender": () => undefined };
const renders = (app: Awaited<ReturnType<typeof brainWith>>) => app.host.posts().filter((post) => post.name === "app.mermaidRender").map((post) => post.data as { source: string; maxWidth: number | null });
const inline = (app: Awaited<ReturnType<typeof brainWith>>) => renders(app).filter((render) => render.maxWidth !== null);

describe("a mermaid block in a reply is drawn as its diagram", () => {
  test("drawn once, with Chinese labels, <br/> and subgraphs as written; Copy copies its source", async () => {
    const app = await brainWith([reply("a1", 1, fence(FLOW))]);
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    // Drawn once, for the card's measured width (a 390 pt phone: 328).
    expect(renders(app)).toEqual([{ source: FLOW, maxWidth: 328 }]);
    expect(app.shows("id:mermaid-code")).toBe(false);
    await app.press("id:mermaid-copy");
    await app.waitUntil(() => app.clipboard() === FLOW);
  }, 20_000);

  test("a tap opens it full screen, fitted; + and − zoom it in steps, Fit returns, × closes", async () => {
    const app = await brainWith([reply("a1", 1, fence(FLOW))]);
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:mermaid-tap");
    await app.waitFor("id:mermaid-viewer-view");
    expect(app.texts()).toContain("Fit");
    // From fitted, + is the drawn size; then a step up and back.
    await app.press("id:mermaid-zoom-in");
    await app.waitFor("text:100%");
    await app.press("id:mermaid-zoom-in");
    await app.waitFor("text:150%");
    await app.press("id:mermaid-zoom-out");
    await app.waitFor("text:100%");
    await app.press("id:mermaid-zoom-fit");
    await app.waitFor("text:Fit");
    await app.press("id:mermaid-viewer-close");
    await app.waitUntil(() => !app.shows("id:mermaid-viewer-view"));
    // Full screen it is drawn once more, as written (null: no width to fit), however it is zoomed.
    expect(renders(app).map((render) => render.maxWidth)).toEqual([328, null]);
  }, 20_000);

  test("a diagram Pele cannot read is its code with the parser's own words, not a blank box", async () => {
    const app = await brainWith([reply("a1", 1, fence("flowchart LR\nA --> BROKEN["))]);
    await app.waitFor("id:mermaid-error", { timeout: 10_000 });
    expect(app.shows("id:mermaid-diagram")).toBe(false);
    expect(app.shows("id:mermaid-code >> id:code-block")).toBe(true);
    expect(app.texts().some((text) => text.startsWith("Parse error on line 2:"))).toBe(true);
  }, 20_000);

  test("a streaming reply shows its code and asks for no drawing until it stops", async () => {
    const app = await brainWith([reply("a1", 1, fence("flowchart LR\n  A --> B"), { partial: true })]);
    await app.waitFor("text:That is all.", { timeout: 10_000 });
    const subscription = framesOf(app.host.posts(), "codex_conversation_subscribe").filter((frame) => frame.worker_id === "brain-host").at(-1)!;
    const delta = (revision: number, body: string, streaming: boolean) => app.host.send("app.daemonPush", { type: "codex_conversation_delta", request_id: subscription.request_id, worker_id: "brain-host", revision, base_revision: revision - 1, deletes: [],
      upserts: [reply("a1", 1, body, { partial: streaming })] });
    await delta(2, fence("flowchart LR\n  A --> B\n  B --> C"), true);
    await delta(3, fence("flowchart LR\n  A --> B\n  B --> C\n  C --> D"), true);
    await app.wait(300);
    expect(renders(app)).toEqual([]);
    expect(app.shows("id:mermaid-diagram")).toBe(false);
    await delta(4, fence("flowchart LR\n  A --> B\n  B --> C\n  C --> D"), false);
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    expect(inline(app).map((render) => render.source)).toEqual(["flowchart LR\n  A --> B\n  B --> C\n  C --> D"]);
  }, 20_000);

  test("switching to Dark recolours the diagram without drawing it again", async () => {
    const app = await brainWith([reply("a1", 1, fence(FLOW))]);
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    await app.setColorScheme("dark");
    await app.wait(300);
    expect(app.shows("id:mermaid-diagram")).toBe(true);
    expect(renders(app)).toHaveLength(1);
  }, 20_000);
});

// Owner 11-16 (a-light-top.png): a wide LR flowchart fitted to a phone had labels some 4 px tall.
describe("at a phone's width a diagram stays legible", () => {
  const WIDE = 'flowchart LR\n  subgraph PH["手机（Mewla App：聊天 + 文件预览 + 终端）"]\n    CH["聊天：Brain / Session 回复<br/>Markdown · 代码块 · 表格"]\n  end\n  subgraph PC["电脑（mewla daemon，新增）"]\n    WS["WebSocket：/ws<br/>session_file_metadata · session_file_text"]\n    FS["文件解析：相对路径按 Session cwd"]\n    AG["Agent：Claude Code / Codex<br/>工作目录 = cwd"]\n  end\n  CH -- "点路径 → 预览" --> WS --> FS --> AG\n  AG -- "回复里写路径" --> CH';
  const SEQ = "sequenceDiagram\n  手机->>Brain: 第 1 步\n  Brain->>Worker: 第 2 步\n  Worker->>daemon: 第 3 步\n  daemon->>GitHub: 第 4 步\n  GitHub->>CI: 第 5 步\n  CI->>Pages: 第 6 步\n  Pages->>Store: 第 7 步";
  const TALL = `flowchart TD\n${Array.from({ length: 30 }, (_, i) => `  N${i}["第 ${i} 步"] --> N${i + 1}`).join("\n")}`;

  test("a small diagram is fitted in the card", async () => {
    const app = await brainWith([reply("a1", 1, fence("flowchart LR\n  A --> B"))]);
    await app.waitFor("id:mermaid-fitted", { timeout: 10_000 });
    expect(app.shows("id:mermaid-scroll")).toBe(false);
    expect(app.shows("id:mermaid-whole")).toBe(true);
  }, 20_000);

  test("a wide LR flowchart is laid out again for the phone (vertical) and fitted at a legible size, not shrunk", async () => {
    const app = await brainWith([reply("a1", 1, fence(WIDE))]);
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    expect(inline(app)).toEqual([{ source: WIDE, maxWidth: 328 }]);
    // Pele turns it vertical (1255 → 357 wide): fitted at 0.92, labels some 13 px; taller than the card, cut there.
    expect(app.shows("id:mermaid-fitted")).toBe(true);
    expect(app.shows("id:mermaid-cropped")).toBe(true);
  }, 20_000);

  test("one that cannot be laid out narrower (eight participants) scrolls sideways at the legible size instead of shrinking", async () => {
    const app = await brainWith([reply("a1", 1, fence(SEQ))]);
    await app.waitFor("id:mermaid-scroll", { timeout: 10_000 });
    expect(app.shows("id:mermaid-fitted")).toBe(false);
  }, 20_000);

  test("a tall one is cut at the card's limit and opens whole", async () => {
    const app = await brainWith([reply("a1", 1, fence(TALL))]);
    await app.waitFor("id:mermaid-cropped", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:mermaid-tap");
    await app.waitFor("id:mermaid-viewer-view");
  }, 20_000);
});
