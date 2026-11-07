import { describe, expect, test } from "bun:test";
import { parseMessageMarkdown } from "../terminal/InterfaceMessageMarkdownModel";
import { isMermaidFenceLanguage } from "./mermaidFences";
import {
  MERMAID_MARKDOWN_FIXTURE,
  PERPETUO_SINGLE_DOMAIN_FLOWCHART,
  SIMPLE_FLOWCHART_TD,
} from "./mermaidFixtures";

describe("Mermaid Markdown fences", () => {
  test("recognizes mermaid language only", () => {
    expect(isMermaidFenceLanguage("mermaid")).toBe(true);
    expect(isMermaidFenceLanguage("MERMAID")).toBe(true);
    expect(isMermaidFenceLanguage("ts")).toBe(false);
    expect(isMermaidFenceLanguage("text")).toBe(false);
  });

  test("reads the Perpetuo fixture's diagrams as mermaid blocks without eating nested example fences in TypeScript", () => {
    const blocks = parseMessageMarkdown(MERMAID_MARKDOWN_FIXTURE);
    const code = blocks.filter((block) => block.type === "code");
    const mermaid = code.filter((block) => block.language === "mermaid");
    expect(mermaid).toHaveLength(2);
    expect(mermaid[0]?.closed).toBe(true);
    expect(mermaid[0]?.text).toContain("subgraph Browser");
    expect(mermaid[1]?.text).toContain("flowchart TD");
    expect(code.some((block) => block.language === "ts")).toBe(true);
  });

  test("a quoted fence stays inside the quote, and an open fence is open", () => {
    const quoted = parseMessageMarkdown("> not a fence\n> ```mermaid\n> flowchart TD\n> A-->B\n> ```\n");
    expect(quoted.map((block) => block.type)).toEqual(["quote"]);
    const open = parseMessageMarkdown("intro\n```mermaid\nflowchart TD\n  A-->B\n");
    expect(open[1]).toMatchObject({ type: "code", language: "mermaid", closed: false, text: "flowchart TD\n  A-->B" });
  });

  test("a closed mermaid fence is a mermaid block that says it is closed", () => {
    const markdown = `Hello\n\n\`\`\`mermaid\n${SIMPLE_FLOWCHART_TD.trim()}\n\`\`\`\n`;
    expect(parseMessageMarkdown(markdown).find((block) => block.type === "code")).toMatchObject({
      type: "code",
      language: "mermaid",
      closed: true,
      text: SIMPLE_FLOWCHART_TD.trim(),
    });
  });

  test("preserves the exact Perpetuo source through the message parser", () => {
    const markdown = `\`\`\`mermaid\n${PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim()}\n\`\`\``;
    const block = parseMessageMarkdown(markdown).find((item) => item.type === "code");
    expect(block?.type === "code" && block.language).toBe("mermaid");
    expect(block?.type === "code" && block.text).toBe(
      PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim(),
    );
  });
});
