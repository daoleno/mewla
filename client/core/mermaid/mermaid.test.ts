import { describe, expect, test } from "bun:test";
import {
  createMermaidRenderer,
  isMermaidLanguage,
  mermaidEngineDocument,
  mermaidSvgDocument,
  mermaidSvgSize,
  parseMermaidEngineMessage,
  prepareMermaid,
  type MermaidEngineLink,
} from "./mermaid";

const SVG = '<svg id="x" width="640" height="320" viewBox="0 0 640 320" xmlns="http://www.w3.org/2000/svg"><g onclick="alert(1)"><text>图</text></g><script>alert(1)</script></svg>';

describe("which blocks are diagrams, and what is drawn", () => {
  test("a block tagged mermaid, whatever its case or info string", () => {
    expect(isMermaidLanguage("mermaid")).toBe(true);
    expect(isMermaidLanguage("Mermaid title=x")).toBe(true);
    expect(isMermaidLanguage("mermaidjs")).toBe(false);
    expect(isMermaidLanguage("sh")).toBe(false);
    expect(isMermaidLanguage(undefined)).toBe(false);
  });

  test("the source keeps Chinese labels, <br/> and subgraphs; config, clicks and other HTML go", () => {
    const raw = '---\ntitle: x\nconfig:\n  theme: forest\n---\n%%{init: {"securityLevel": "loose"}}%%\nflowchart LR\n  subgraph DP["数据平台<br>RDS"]\n    A["源 Space<b>x</b>"] --> B\n  end\n  click A "javascript:alert(1)"\n  B -- "写派生 Version" --> C<img src=x onerror=alert(1)>\n';
    const prepared = prepareMermaid(raw);
    expect(prepared).toEqual({ ok: true, source: 'flowchart LR\n  subgraph DP["数据平台<br/>RDS"]\n    A["源 Spacex"] --> B\n  end\n\n  B -- "写派生 Version" --> C' });
  });

  test("an empty or oversized diagram says why it is not drawn", () => {
    expect(prepareMermaid("  \n%%{init:{}}%%\n")).toEqual({ ok: false, error: "The diagram is empty." });
    const big = prepareMermaid(`flowchart TD\n${"A-->B\n".repeat(6000)}`);
    expect(big.ok).toBe(false);
  });

  test("the engine's answers: a drawing with its size, cleaned; mermaid's own words for an error", () => {
    expect(mermaidSvgSize(SVG)).toEqual({ width: 640, height: 320 });
    const drawn = parseMermaidEngineMessage(JSON.stringify({ type: "result", id: "m1", ok: true, svg: SVG }));
    expect(drawn?.type).toBe("result");
    if (drawn?.type !== "result" || !drawn.result.ok) throw new Error("not drawn");
    expect(drawn.result.svg).not.toContain("<script");
    expect(drawn.result.svg).not.toContain("onclick");
    expect(drawn.result).toMatchObject({ width: 640, height: 320 });
    const error = "Parse error on line 3:\n...B --> \n-----------^\nExpecting 'AMP', got 'EOF'";
    expect(parseMermaidEngineMessage({ type: "result", id: "m2", ok: false, error })).toEqual({ type: "result", id: "m2", result: { ok: false, error } });
    expect(parseMermaidEngineMessage({ type: "result", id: "m3", ok: true, svg: "" })).toEqual({ type: "result", id: "m3", result: { ok: false, error: "The diagram could not be drawn." } });
    expect(parseMermaidEngineMessage("not json")).toBeNull();
    expect(parseMermaidEngineMessage({ type: "ready" })).toEqual({ type: "ready", ok: true, error: undefined });
  });
});

/** A renderer on an engine that answers when the test says. */
function engine(options: { timeoutMs?: number; engineTimeoutMs?: number } = {}) {
  const sent: Array<{ id: string; source: string; mode: string; themeVariables: Record<string, unknown> }> = [];
  let wanted = 0;
  const renderer = createMermaidRenderer({ themeVariables: (mode) => ({ darkMode: mode === "dark" }), onWanted: () => wanted++, ...options });
  const link: MermaidEngineLink = { send: (message) => sent.push(JSON.parse(message)) };
  const answer = (index: number, body: Record<string, unknown>) => renderer.receive(JSON.stringify({ type: "result", id: sent[index].id, ...body }));
  return { renderer, link, sent, answer, wanted: () => wanted };
}

describe("drawing, one at a time, kept", () => {
  test("nothing is drawn until asked; the engine is wanted then and drawings wait for it to be ready", async () => {
    const t = engine();
    expect(t.renderer.wanted()).toBe(false);
    const first = t.renderer.render("flowchart LR\n A-->B", "light");
    const second = t.renderer.render("sequenceDiagram\n A->>B: hi", "dark");
    expect(t.wanted()).toBe(1);
    t.renderer.attach(t.link);
    expect(t.sent).toEqual([]);
    t.renderer.receive({ type: "ready", ok: true });
    expect(t.sent.map((m) => [m.source, m.mode, m.themeVariables.darkMode])).toEqual([["flowchart LR\n A-->B", "light", false]]);
    t.answer(0, { ok: true, svg: SVG });
    expect(await first).toMatchObject({ ok: true, width: 640 });
    expect(t.sent).toHaveLength(2);
    t.answer(1, { ok: false, error: "Parse error on line 2" });
    expect(await second).toEqual({ ok: false, error: "Parse error on line 2" });
  });

  test("a drawing (or mermaid's error) is kept: asked again, nothing is sent", async () => {
    const t = engine();
    t.renderer.attach(t.link);
    t.renderer.receive({ type: "ready" });
    const a = t.renderer.render("graph TD\n A", "light");
    const again = t.renderer.render("graph TD\n A", "light");
    expect(again).toBe(a);
    t.answer(0, { ok: false, error: "bad" });
    await a;
    expect(t.renderer.cached("graph TD\n A", "light")).toEqual({ ok: false, error: "bad" });
    await t.renderer.render("graph TD\n A", "light");
    expect(t.sent).toHaveLength(1);
    // The other theme is its own drawing.
    void t.renderer.render("graph TD\n A", "dark");
    expect(t.sent).toHaveLength(2);
  });

  test("an answer for another drawing is ignored; a drawing that takes too long fails and is not kept", async () => {
    const t = engine({ timeoutMs: 10 });
    t.renderer.attach(t.link);
    t.renderer.receive({ type: "ready" });
    const slow = t.renderer.render("graph TD\n A", "light");
    t.renderer.receive({ type: "result", id: "elsewhere", ok: true, svg: SVG });
    expect(await slow).toEqual({ ok: false, error: "Drawing the diagram took too long." });
    expect(t.renderer.cached("graph TD\n A", "light")).toBeUndefined();
    void t.renderer.render("graph TD\n A", "light");
    expect(t.sent).toHaveLength(2);
  });

  test("an engine that never starts, or says it could not, fails what waits with its words", async () => {
    const never = engine({ engineTimeoutMs: 10 });
    expect(await never.renderer.render("graph TD\n A", "light")).toEqual({ ok: false, error: "The diagram engine did not start." });
    const broken = engine();
    broken.renderer.attach(broken.link);
    const waiting = broken.renderer.render("graph TD\n A", "light");
    broken.renderer.receive({ type: "ready", ok: false, error: "The diagram engine did not load." });
    expect(await waiting).toEqual({ ok: false, error: "The diagram engine did not load." });
    expect(await broken.renderer.render("graph TD\n B", "light")).toEqual({ ok: false, error: "The diagram engine did not load." });
  });

  test("an engine that goes mid-drawing hands it to the next one", async () => {
    const t = engine();
    t.renderer.attach(t.link);
    t.renderer.receive({ type: "ready" });
    const drawing = t.renderer.render("graph TD\n A", "light");
    t.renderer.attach(null);
    t.renderer.attach(t.link);
    t.renderer.receive({ type: "ready" });
    expect(t.sent).toHaveLength(2);
    t.answer(1, { ok: true, svg: SVG });
    expect((await drawing).ok).toBe(true);
  });
});

describe("the documents", () => {
  test("the engine's runs mermaid inline and loads nothing", () => {
    const html = mermaidEngineDocument("window.mermaid={render:function(){}};");
    expect(html).toContain("script-src 'unsafe-inline'");
    expect(html).toContain("connect-src 'none'");
    expect(html).not.toMatch(/<script[^>]+src=/i);
  });

  test("a diagram's runs no script; fitted, or at a scale to pan", () => {
    const shown = mermaidSvgDocument({ svg: "<svg viewBox='0 0 640 320'></svg>", width: 640, height: 320 }, "#FFFFFF");
    expect(shown).toContain("script-src 'none'");
    expect(shown).toContain("max-width:640px");
    expect(mermaidSvgDocument({ svg: "<svg></svg>", width: 640, height: 320 }, "#000000", 1.5)).toContain("width:960px;height:480px");
    expect(mermaidSvgDocument({ svg: "<svg></svg>", width: 1, height: 1 }, "red;}</style><script>")).toContain("background:transparent");
  });
});
