import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  drawMermaid,
  isMermaidLanguage,
  MERMAID_FONT_PX,
  MERMAID_MIN_LABEL_PX,
  mermaidCssVariables,
  mermaidInlineLayout,
  mermaidSvgDocument,
  prepareMermaid,
  sanitizeMermaidSvg,
  setMermaidRenderSink,
  type MermaidDrawing,
} from "./mermaid";

// The shapes Brain's designs have (owner 11-16, DESIGN.md / DESIGN-v2.md): `flowchart LR`, subgraphs, Chinese labels,
// `<br/>`, long labels. Written for these tests: the real ones are another project's.
const fixture = (name: string) => readFileSync(join(import.meta.dir, "fixtures", name), "utf8");
const WIDE = [fixture("wide-lr.mmd"), fixture("wide-graph.mmd")];
const PHONE = 326;
const PALETTE = { surface: "#FFFFFF", ink: "#131416", soft: "#62656D", node: "#EDEDEE", border: "#A6A9B0", cluster: "#F7F7F8" };

const drawn = (source: string, width: number | null): MermaidDrawing => {
  const result = drawMermaid(source, width);
  if (!result.ok) throw new Error(result.error);
  return result;
};

describe("which blocks are diagrams, and what is drawn", () => {
  test("a block tagged mermaid, whatever its case or info string", () => {
    expect(isMermaidLanguage("mermaid")).toBe(true);
    expect(isMermaidLanguage("Mermaid title=x")).toBe(true);
    expect(isMermaidLanguage("mermaidjs")).toBe(false);
    expect(isMermaidLanguage(undefined)).toBe(false);
  });

  test("an empty or oversized diagram says why it is not drawn", () => {
    expect(prepareMermaid("  \n ")).toEqual({ ok: false, error: "The diagram is empty." });
    expect(prepareMermaid(`flowchart TD\n${"A-->B\n".repeat(6000)}`).ok).toBe(false);
  });

  test("the diagram types agents write draw, with Chinese labels and <br/> as written", () => {
    const cases: Record<string, string> = {
      flowchart: WIDE[0],
      graph: "graph TB\n  a-->b & c-->d",
      sequence: "sequenceDiagram\n  autonumber\n  actor U as 用户\n  U->>+A: 请求\n  Note over U,A: 备注\n  alt ok\n    A-->>-U: 200\n  else fail\n    A--xU: 500\n  end",
      state: "stateDiagram-v2\n  [*] --> Active\n  state Active {\n    [*] --> Idle\n    Idle --> Busy: job\n  }\n  Active --> [*]",
      gantt: "gantt\n  title 计划\n  dateFormat YYYY-MM-DD\n  section 第一步\n  搜索主机 :a1, 2026-11-17, 3d\n  MCP 工具 :after a1, 2d",
      class: "classDiagram\n  class Animal {\n    +String name\n    +eat() void\n  }\n  Animal <|-- Dog",
      er: "erDiagram\n  USER ||--o{ ORDER : places",
      pie: 'pie title 占比\n  "A" : 40\n  "B" : 60',
      mindmap: "mindmap\n  root((中心))\n    A\n    B",
      timeline: "timeline\n  title 时间线\n  2026 : 立项",
      config: "---\ntitle: 标题\n---\n%%{init: {'theme':'dark'}}%%\nflowchart LR\n  A[\"<b>粗</b><br/>下一行\"] --> B\n  click A \"https://example.com\"",
    };
    for (const [type, source] of Object.entries(cases)) {
      const result = drawMermaid(source, PHONE);
      expect(result.ok ? result.svg.includes("<svg") : result.error).toBe(true);
    }
    const flow = drawn(WIDE[0], PHONE);
    expect(flow.svg).toContain("新增");
    expect(flow.svg).not.toContain("&lt;br");
  });

  test("a diagram it cannot read keeps the parser's own words", () => {
    const result = drawMermaid("flowchart LR\n  A -->\n  B[", PHONE);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error).toMatch(/^Parse error on line 3:[\s\S]*Expecting/);
  });

  test("what is shown runs nothing: no script, handler, foreignObject or script link", () => {
    expect(sanitizeMermaidSvg('<svg><g onclick="x()"><a href="javascript:alert(1)">t</a></g><script>x()</script><foreignObject><div/></foreignObject></svg>'))
      .toBe('<svg><g><a>t</a></g></svg>');
    const evil = drawn('flowchart LR\n  A["<img src=x onerror=alert(1)>"] --> B\n  click A "javascript:alert(1)"', PHONE);
    expect(evil.svg).not.toMatch(/<img|onerror=|javascript:/i);
  });
});

describe("drawn once, and only when asked", () => {
  test("each (source, width) is drawn once and kept; another width is its own drawing", () => {
    const seen: unknown[] = [];
    setMermaidRenderSink((entry) => seen.push(entry));
    try {
      const source = "flowchart LR\n  once --> kept";
      drawMermaid(source, PHONE);
      drawMermaid(source, PHONE);
      expect(seen).toEqual([{ source, maxWidth: PHONE }]);
      drawMermaid(source, null);
      expect(seen).toHaveLength(2);
    } finally {
      setMermaidRenderSink(null);
    }
  });
});

describe("legible at a phone's width (owner 11-16: a wide flowchart shrank its labels to some 4 px)", () => {
  test("a wide LR flowchart is laid out again for a phone, and its labels are never shown under MIN_LABEL_PX", () => {
    for (const source of WIDE) {
      const asWritten = drawn(source, null);
      const phone = drawn(source, PHONE);
      expect(phone.width).toBeLessThan(asWritten.width);
      const layout = mermaidInlineLayout(phone, PHONE, 420);
      expect(layout.scale * MERMAID_FONT_PX).toBeGreaterThanOrEqual(MERMAID_MIN_LABEL_PX);
      // Fitted as it was drawn before, its labels were tiny.
      expect((PHONE / asWritten.width) * MERMAID_FONT_PX).toBeLessThan(MERMAID_MIN_LABEL_PX);
    }
  });

  test("fitted while legible; wider scrolls sideways at the legible scale; taller is cut; never larger than drawn", () => {
    expect(mermaidInlineLayout({ width: 200, height: 100 }, PHONE, 420)).toEqual({ scale: 1, width: 200, height: 100, frameHeight: 100, scrolls: false, cropped: false });
    const fits = mermaidInlineLayout({ width: 400, height: 200 }, PHONE, 420);
    expect(fits.scrolls).toBe(false);
    expect(fits.width).toBeLessThanOrEqual(PHONE);
    const wide = mermaidInlineLayout({ width: 1500, height: 400 }, PHONE, 420);
    expect(wide).toMatchObject({ scale: MERMAID_MIN_LABEL_PX / MERMAID_FONT_PX, scrolls: true });
    expect(wide.width).toBeGreaterThan(PHONE);
    const tall = mermaidInlineLayout({ width: 300, height: 1200 }, PHONE, 420);
    expect(tall).toMatchObject({ frameHeight: 420, cropped: true, scrolls: false });
  });
});

describe("the view", () => {
  test("runs no script and is coloured by Pele's CSS variables: light and dark are one drawing", () => {
    const flow = drawn("flowchart LR\n  A --> B", PHONE);
    const light = mermaidSvgDocument(flow, PALETTE, 1, true);
    const dark = mermaidSvgDocument(flow, { ...PALETTE, surface: "#1C1D20", ink: "#F2F3F5" }, 1, true);
    expect(light).toContain("script-src 'none'");
    expect(light).toContain("--pele-fg:#131416");
    expect(dark).toContain("--pele-fg:#F2F3F5");
    expect(light.replace(/<style>[\s\S]*<\/style>/, "")).toBe(dark.replace(/<style>[\s\S]*<\/style>/, ""));
    expect(flow.svg).toContain("var(--pele-fg");
  });

  test("a colour that is not one never reaches the page", () => {
    expect(mermaidCssVariables({ ...PALETTE, ink: "red;}</style><script>" })).toContain("--pele-fg:#808080");
    expect(mermaidSvgDocument({ svg: "<svg></svg>", width: 640, height: 320 }, PALETTE, 1.5)).toContain("width:960px!important;height:480px!important");
  });
});
