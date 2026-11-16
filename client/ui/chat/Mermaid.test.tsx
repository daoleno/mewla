import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

// Owner 11-16: 「flowchart 在 mewla 里也要支持一下」. The engine is faked (mermaid/testing.ts): a headless test runs no web view.
const APP = "../FakeDaemonApp.tsx";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const at = (s: number) => `2026-11-16T07:00:${String(s).padStart(2, "0")}.000Z`;
const reply = (id: string, seq: number, body: string, extra: Record<string, unknown> = {}) => ({ id, seq, timestamp: at(seq), kind: "assistant_message", role: "assistant", body, ...extra });
const FLOW = 'flowchart LR\n  subgraph DP["data-platform（权威）"]\n    SRC["源 Space<br/>OpenList"]\n  end\n  SRC -- "API 读源 Version" --> W["derive-worker"]';
const fence = (source: string) => `The design:\n\n\`\`\`mermaid\n${source}\n\`\`\`\n\nThat is all.`;

async function brainWith(events: Record<string, unknown>[]) {
  const host = phoneHost({ brain, conversations: { "brain-host": events } }, RECORD);
  const app = await open(APP, { platform: "android", storage: paired, host: { ...host, params: { ...host.params, mermaid: {} } } });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

const RECORD = { "app.mermaidRender": () => undefined };
const renders = (app: Awaited<ReturnType<typeof brainWith>>) => app.host.posts().filter((post) => post.name === "app.mermaidRender").map((post) => post.data as { source: string; mode: string });

describe("a mermaid block in a reply is drawn as its diagram", () => {
  test("drawn once, with Chinese labels, <br/> and subgraphs as written; Copy copies its source", async () => {
    const app = await brainWith([reply("a1", 1, fence(FLOW))]);
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    expect(renders(app)).toEqual([{ source: FLOW, mode: "light" }]);
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
    // Opened and zoomed, it was drawn only once.
    expect(renders(app)).toHaveLength(1);
  }, 20_000);

  test("a diagram mermaid cannot read is its code with mermaid's own words, not a blank box", async () => {
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
    expect(renders(app).map((render) => render.source)).toEqual(["flowchart LR\n  A --> B\n  B --> C\n  C --> D"]);
  }, 20_000);

  test("in Dark it is drawn in the dark theme", async () => {
    const host = phoneHost({ brain, conversations: { "brain-host": [reply("a1", 1, fence(FLOW))] } }, RECORD);
    const app = await open(APP, { platform: "android", colorScheme: "dark", storage: paired, host: { ...host, params: { ...host.params, mermaid: {} } } });
    await app.waitFor("id:mermaid-diagram", { timeout: 10_000 });
    expect(renders(app).map((render) => render.mode)).toEqual(["dark"]);
  }, 20_000);
});
