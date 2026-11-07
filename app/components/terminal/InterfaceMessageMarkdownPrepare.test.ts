import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { prepareInterfaceMarkdown } from "./InterfaceMessageMarkdownPrepare";

const read = (path: string) => readFileSync(join(import.meta.dir, path), "utf8");

describe("Interface Markdown: one reader on every platform", () => {
  test("makes prose selectable before the native long-press begins", () => {
    const selectionOwner = read("InterfaceChatSurfaceHooks.ts");
    expect(selectionOwner).toContain("const textSelectable = true;");
    expect(selectionOwner).not.toContain("[textSelectable, setTextSelectable]");
    expect(read("InterfaceMessageBlock.tsx")).toContain("useTimelineSelectableTextProps()");

    const touchOwnership = selectionOwner.slice(
      selectionOwner.indexOf("const handleTimelineTouchActiveChange"),
      selectionOwner.indexOf("const attachToLatest"),
    );
    const selectionOwnership = selectionOwner.slice(
      selectionOwner.indexOf("const handleTextSelectionGestureStart"),
      selectionOwner.indexOf("const handleTextSelectionGestureEnd"),
    );
    expect(touchOwnership).not.toContain("cancelTurnFocus");
    expect(selectionOwnership).not.toContain("cancelTurnFocus");
    expect(selectionOwner).toContain('cancelTurnFocus("drag")');
  });

  test("no second Markdown renderer or platform fork exists", () => {
    for (const file of [
      "InterfaceNativeMarkdownBody.tsx",
      "InterfaceNativeMarkdownBody.android.tsx",
      "InterfaceNativeMarkdownBody.ios.tsx",
      "InterfaceMessageBodyModel.ts",
    ]) {
      expect(existsSync(join(import.meta.dir, file))).toBe(false);
    }
    const app = JSON.parse(readFileSync(join(import.meta.dir, "../../package.json"), "utf8"));
    expect(app.dependencies["react-native-enriched-markdown"]).toBeUndefined();
    expect(app.dependencies.marked).toBeDefined();
  });

  test("normalizes markdown for the reader", () => {
    expect(prepareInterfaceMarkdown("  hello  ", false)).toBe("hello");
    expect(
      prepareInterfaceMarkdown("![alt](https://example.com/x.png)", false),
    ).toBe("![alt](https://example.com/x.png)");
  });

  test("keeps inline code without opaque fragment backgrounds", () => {
    const codeOwner = read("InterfaceInlineCodeStyle.ts");
    expect(read("InterfaceInlineMessage.tsx")).toContain("interfaceInlineCodeStyle(theme, compact)");
    expect(codeOwner).toContain('backgroundColor: "transparent"');
    expect(codeOwner).toContain('borderColor: "transparent"');
    expect(codeOwner).toContain("theme.cyan");
    expect(codeOwner).not.toContain("surfaceMuted");
  });
});
