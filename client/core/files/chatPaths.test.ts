import { describe, expect, test } from "bun:test";
import { linkFilePaths, namedHomeFile } from "./chatPaths";
import { recognizeSessionFileReference } from "./sessionFilePreview";

const PDF = "/home/daoleno/.mewla/brain/workspace/worklog/0g-pay-full-build-1115/0G-Pay-Unifra-Plan-zh.pdf";

/** The destinations of the links a reply's Markdown has, as the chat's link handler receives them. */
function destinations(markdown: string): string[] {
  return [...markdown.matchAll(/\]\(<((?:\\.|[^>\\])*)>\)/g)].map((m) => m[1].replace(/\\(.)/g, "$1"));
}

describe("files a reply names become links (owner 11-15)", () => {
  test("a home path in inline code links to itself, its code look kept", () => {
    const reply = `计划书已生成：\`${PDF}\``;
    expect(linkFilePaths(reply)).toBe(`计划书已生成：[\`${PDF}\`](<${PDF}>)`);
    expect(destinations(linkFilePaths(reply)).map(recognizeSessionFileReference)).toEqual([PDF]);
  });

  test("a home path in running text, Chinese around it with no spaces, trailing punctuation left out", () => {
    expect(destinations(linkFilePaths(`见${PDF}。`))).toEqual([PDF]);
    expect(destinations(linkFilePaths(`文件在${PDF}里面，请看`))).toEqual([PDF]);
    expect(destinations(linkFilePaths(`(see ${PDF}).`))).toEqual([PDF]);
    expect(destinations(linkFilePaths(`- **Plan**: ${PDF}, and ~/notes/todo.md`))).toEqual([PDF, "~/notes/todo.md"]);
    expect(linkFilePaths(`Read ${PDF}.`)).toBe(`Read [${PDF}](<${PDF}>).`);
  });

  test("CJK names, spaces in a code span, line suffixes, macOS and Windows homes", () => {
    expect(destinations(linkFilePaths("报告在 /home/me/文档/季度报告.md 里"))).toEqual(["/home/me/文档/季度报告.md"]);
    expect(destinations(linkFilePaths("`/Users/me/My Files/shot 1.png`"))).toEqual(["/Users/me/My Files/shot 1.png"]);
    expect(destinations(linkFilePaths("at /home/me/src/main.go:42 now"))).toEqual(["/home/me/src/main.go:42"]);
    expect(recognizeSessionFileReference("/home/me/src/main.go:42")).toBe("/home/me/src/main.go");
    const windows = "C:\\Users\\me\\Desktop\\a.png";
    expect(linkFilePaths(`\`${windows}\``)).toBe(`[\`${windows}\`](<C:\\\\Users\\\\me\\\\Desktop\\\\a.png>)`);
    expect(destinations(linkFilePaths(`\`${windows}\``))).toEqual([windows]);
    expect(destinations(linkFilePaths("/root/out/report.html"))).toEqual(["/root/out/report.html"]);
  });

  test("code blocks, links, URLs and paths outside a home stay as they are", () => {
    const untouched = [
      "```\ncat /home/me/a.pdf\n```",
      "~~~sh\nopen /home/me/a.pdf\n~~~",
      "Text\n\n    /home/me/indented-code.pdf",
      `[the plan](${PDF})`,
      `[\`${PDF}\`](${PDF})`,
      "![shot](/home/me/shot.png)",
      "https://example.com/home/me/a.pdf",
      "<https://example.com/home/me/a.pdf>",
      "call /api/v1/users.json, then /tmp/out.pdf and `/etc/hosts.conf`",
      "relative docs/a.md and a/home/me/b.pdf",
      "the folder `/home/me/workspace` and /home/me/src",
    ];
    for (const reply of untouched) expect(linkFilePaths(reply)).toBe(reply);
  });

  test("a path in a list item's indented continuation still links", () => {
    expect(destinations(linkFilePaths("- Files:\n\n    - `/home/me/a.md`"))).toEqual(["/home/me/a.md"]);
  });

  test("a code span is linked only when it is one path", () => {
    expect(namedHomeFile("/home/me/a.pdf")).toBe("/home/me/a.pdf");
    expect(namedHomeFile(" ~/b.txt ")).toBe("~/b.txt");
    expect(namedHomeFile("open /home/me/a.pdf")).toBeNull();
    expect(namedHomeFile("/home/me/")).toBeNull();
    expect(linkFilePaths("run `open /home/me/a.pdf`")).toBe("run `open /home/me/a.pdf`");
  });

  test("a reply naming no home path is returned as it is, and a long one stays fast", () => {
    const plain = "Nothing to open here.";
    expect(linkFilePaths(plain)).toBe(plain);
    const long = Array.from({ length: 2000 }, (_, i) => `Line ${i}: see \`/home/me/f${i}.md\` and /home/me/g${i}.pdf.`).join("\n");
    const started = performance.now();
    const linked = linkFilePaths(long);
    expect(performance.now() - started).toBeLessThan(100);
    expect(destinations(linked).length).toBe(4000);
  });
});
