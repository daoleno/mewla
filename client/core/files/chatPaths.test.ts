import { describe, expect, test } from "bun:test";
import { chatFileCandidates, fileKey, linkFilePaths, MAX_CHAT_FILE_CANDIDATES, namedHomeFile } from "./chatPaths";
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

  test("two paths with only Chinese between them are two links (Brain 11-15)", () => {
    expect(destinations(linkFilePaths("见/home/me/a.pdf和/home/me/b.pdf。"))).toEqual(["/home/me/a.pdf", "/home/me/b.pdf"]);
    expect(destinations(linkFilePaths("~/x/报告.md与~/y/图.png"))).toEqual(["~/x/报告.md", "~/y/图.png"]);
    // A CJK folder or file name inside a path stays one path.
    expect(destinations(linkFilePaths("在 /home/me/文档/季度报告.pdf里"))).toEqual(["/home/me/文档/季度报告.pdf"]);
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
      "relative docs/a.md and a/home/me/b.pdf", // not known to be files: see "paths that are not a home's" below
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

describe("paths that are not a home's link once the computer said they are files (owner 11-16)", () => {
  // Brain's reply the owner could not open: a path relative to Brain's workspace, in inline code.
  const DESIGN = "worklog/freeride-data-search-1115/DESIGN-v2.md";
  const reply = `After your feedback I rewrote it as v2 (\`${DESIGN}\`):`;

  test("not known, they stay as they are; known, they link to themselves", () => {
    expect(linkFilePaths(reply)).toBe(reply);
    expect(linkFilePaths(reply, new Set(["worklog/other.md"]))).toBe(reply);
    expect(linkFilePaths(reply, new Set([DESIGN]))).toBe(`After your feedback I rewrote it as v2 ([\`${DESIGN}\`](<${DESIGN}>)):`);
    expect(destinations(linkFilePaths(reply, new Set([DESIGN]))).map(recognizeSessionFileReference)).toEqual([DESIGN]);
  });

  test("each form: relative, ./ and ../, a bare name in code, :line and #L, an absolute path outside a home", () => {
    const cases: Array<[string, string, string]> = [
      // [reply, the path asked about, the link's destination]
      ["see worklog/a-1116/RESULT.md.", "worklog/a-1116/RESULT.md", "worklog/a-1116/RESULT.md"],
      ["`app/ui-v2/DESIGN-DIFF.md`", "app/ui-v2/DESIGN-DIFF.md", "app/ui-v2/DESIGN-DIFF.md"],
      ["run ./scripts/run.sh now", "./scripts/run.sh", "./scripts/run.sh"],
      ["`../notes/plan.md`", "../notes/plan.md", "../notes/plan.md"],
      ["the brief in `RESULT.md`", "RESULT.md", "RESULT.md"],
      ["at providers/tushare.ts:4", "providers/tushare.ts", "providers/tushare.ts:4"],
      ["`apps/harness/src/mini-apps.ts:273:9`", "apps/harness/src/mini-apps.ts", "apps/harness/src/mini-apps.ts:273:9"],
      ["`client/core/a.ts#L12`", "client/core/a.ts", "client/core/a.ts#L12"],
      ["截图在/tmp/q-dark.png里", "/tmp/q-dark.png", "/tmp/q-dark.png"],
      ["`/etc/systemd/zram-generator.conf`", "/etc/systemd/zram-generator.conf", "/etc/systemd/zram-generator.conf"],
      ["方案见worklog/x/方案.md，请看", "worklog/x/方案.md", "worklog/x/方案.md"],
      ["**Plan:** worklog/x/a.md和worklog/y/b.md", "worklog/x/a.md", "worklog/x/a.md"],
    ];
    for (const [text, asked, linked] of cases) {
      expect(chatFileCandidates(text)).toContain(asked);
      expect(fileKey(linked)).toBe(asked);
      expect(destinations(linkFilePaths(text, new Set([asked])))).toEqual([linked]);
      expect(recognizeSessionFileReference(linked)).toBe(asked);
    }
    expect(destinations(linkFilePaths("**Plan:** worklog/x/a.md和worklog/y/b.md", new Set(["worklog/x/a.md", "worklog/y/b.md"])))).toEqual(["worklog/x/a.md", "worklog/y/b.md"]);
    // A path not known before a Chinese join does not hide the next one.
    expect(destinations(linkFilePaths("见docs/a.md和/home/me/b.pdf"))).toEqual(["/home/me/b.pdf"]);
  });

  test("code blocks, links, URLs, elided and non-file text are never asked about nor linked", () => {
    const known = new Set(["docs/a.md", "a.md", "RESULT.md", "x/y.md"]);
    const untouched = [
      "```\ncat docs/a.md\n```",
      "Text\n\n    docs/a.md",
      "[the doc](docs/a.md)",
      "[`docs/a.md`](docs/a.md)",
      "https://example.com/docs/a.md and <https://example.com/x/y.md>",
      "`worklog/.../DESIGN-v2.md` and .../RESULT.md",
      "`127.0.0.1:8317`, `v0.3.0`, and/or 11/16",
      "a bare RESULT.md in text",
      "`run docs/a.md`",
    ];
    for (const reply of untouched) {
      expect(linkFilePaths(reply, known)).toBe(reply);
    }
    expect(chatFileCandidates("```\ncat docs/a.md\n```\n[x](docs/a.md) https://e.com/x/y.md `127.0.0.1:8317` `worklog/.../a.md`")).toEqual([]);
    expect(chatFileCandidates("home paths are linked at once: `/home/me/a.md` ~/b.md")).toEqual([]);
  });

  test("candidates are each asked once, at most a reply's share", () => {
    expect(chatFileCandidates("`a/b.md` a/b.md a/b.md:3 `c.md`")).toEqual(["a/b.md", "c.md"]);
    const many = Array.from({ length: 200 }, (_, i) => `docs/f${i}.md`).join(" ");
    expect(chatFileCandidates(many)).toHaveLength(MAX_CHAT_FILE_CANDIDATES);
  });
  test("a folder named in code is asked about with its slash (a folder report opens with its index.html); in text it is not", () => {
    expect(chatFileCandidates("| (c) | `reports/q3-folder/` |\n\nsee reports/q3-folder/ and `./out/` and `a/../b/` and `https://x.example/a/`")).toEqual(["reports/q3-folder/", "./out/"]);
    expect(linkFilePaths("open `reports/q3-folder/`", new Set(["reports/q3-folder/"]))).not.toBe("open `reports/q3-folder/`");
    expect(linkFilePaths("open `reports/q3-folder/`", new Set())).toBe("open `reports/q3-folder/`");
  });
});
