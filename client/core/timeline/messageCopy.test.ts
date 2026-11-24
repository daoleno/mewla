import { describe, expect, test } from "bun:test";
import { messageCopyText } from "./messageCopy";

const reply = (lines: string[]) => messageCopyText(lines.join("\n"), true);

describe("Copy message: a reply as plain text", () => {
  test("headings and paragraphs lose their marks and stay a blank line apart", () => {
    expect(reply(["## Deploy check", "", "The fix is **in**. 窗口现在按整分钟计算。", "", "Next *step*."]))
      .toBe("Deploy check\n\nThe fix is in. 窗口现在按整分钟计算。\n\nNext step.");
  });

  test("a numbered list keeps its numbers; a bullet is the thread's •; a task its box", () => {
    expect(reply(["1. `SlidingWindow.allow()` counts", "2. covers 59.999 s", "", "- one", "  * nested", "- [ ] todo", "- [x] done"]))
      .toBe("1. SlidingWindow.allow() counts\n2. covers 59.999 s\n\n• one\n  • nested\n☐ todo\n☑ done");
    expect(reply(["3) third"])).toBe("3. third");
  });

  test("code: inline code its text, a block its lines as written (indent kept), no fences", () => {
    expect(reply(["Run it:", "", "```bash", "cd ~/work && go test ./... -run 'A|B'", "    grep -c PASS /tmp/x.log", "", "echo **not bold** `x`", "```", "", "Then `bun run dev`."]))
      .toBe("Run it:\n\ncd ~/work && go test ./... -run 'A|B'\n    grep -c PASS /tmp/x.log\n\necho **not bold** `x`\n\nThen bun run dev.");
    expect(reply(["~~~", "a ~~b~~", "~~~"])).toBe("a ~~b~~");
    expect(reply(["`` a `tick` b ``"])).toBe("a `tick` b");
  });

  test("a table: cells tab-separated, a row a line, the delimiter row gone", () => {
    expect(reply(["| Check | Before | After |", "| --- | :---: | ---: |", "| Requests **kept** | 59 / 60 | 60 / 60 |", "| `a\\|b` | 1 | 2 |", "", "After."]))
      .toBe("Check\tBefore\tAfter\nRequests kept\t59 / 60\t60 / 60\na|b\t1\t2\n\nAfter.");
    expect(reply(["a | b", "--|--", "1 | 2"])).toBe("a\tb\n1\t2");
  });

  test("links keep their address after their words, unless the words are it; pictures their alt text", () => {
    expect(reply(["see [the proxy log](https://example.com/logs/proxy-1340) for 13:40"]))
      .toBe("see the proxy log (https://example.com/logs/proxy-1340) for 13:40");
    expect(reply(["[https://example.com](https://example.com/) and <https://a.b/c>"])).toBe("https://example.com and https://a.b/c");
    expect(reply(["[top](#deploy)"])).toBe("top");
    expect(reply(["![a chart](chart.png)"])).toBe("a chart");
  });

  test("quotes, rules and setext underlines leave no marks; snake_case, paths and escapes stay as meant", () => {
    expect(reply(["> quoted *line*", "", "---", "", "Title", "=====", "", "snake_case_name and ~/a_b/c_d.md, 2 * 3 * 4, \\*literal\\*"]))
      .toBe("quoted line\n\nTitle\n\nsnake_case_name and ~/a_b/c_d.md, 2 * 3 * 4, *literal*");
  });

  test("emoji and line breaks inside a paragraph stay", () => {
    expect(reply(["kept aside. 👍", "next line"])).toBe("kept aside. 👍\nnext line");
  });

  test("the person's own message is copied as typed, marks and all", () => {
    expect(messageCopyText("1. the window\n2. `npm start` → **bun run dev**\n", false)).toBe("1. the window\n2. `npm start` → **bun run dev**\n");
  });
});
