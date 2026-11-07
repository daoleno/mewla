import { describe, expect, test } from "bun:test";
import { buildGitDiffFileActions } from "./gitDiffActions";

describe("Git diff file actions", () => {
  const noop = () => {};
  test("row menu offers view, open, copy path and copy diff; reader adds find and display", () => {
    const base = { path: "src/app.ts", deleted: false, onOpenFile: noop, onCopyPath: noop, onCopyPatch: noop, onViewDiff: noop, onToggleSearch: noop, onToggleOptions: noop };
    expect(buildGitDiffFileActions({ ...base, reading: false }).map((item) => item.key)).toEqual(["view", "open", "copy-path", "copy-diff"]);
    expect(buildGitDiffFileActions({ ...base, reading: true }).map((item) => item.key)).toEqual(["find", "display", "open", "copy-path", "copy-diff"]);
  });
  test("a deleted file cannot open a working copy", () => {
    const items = buildGitDiffFileActions({ path: "gone.ts", deleted: true, reading: false, onOpenFile: noop, onCopyPath: noop, onCopyPatch: noop });
    expect(items.find((item) => item.key === "open")?.disabled).toBe(true);
  });
  test("copy actions receive the file path", () => {
    const copied: string[] = [];
    const items = buildGitDiffFileActions({ path: "src/app.ts", deleted: false, reading: false, onOpenFile: noop, onCopyPath: (path) => copied.push(`path:${path}`), onCopyPatch: (path) => copied.push(`diff:${path}`) });
    for (const item of items) if (item.key.startsWith("copy")) item.onPress();
    expect(copied).toEqual(["path:src/app.ts", "diff:src/app.ts"]);
  });
});
