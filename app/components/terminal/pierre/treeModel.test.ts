import { describe, expect, test } from "bun:test";
import { LONG_PRESS_SLOP_PX, decorationParts, movedPastSlop, treeGitStatus } from "./runtime/treeModel";

const colors = { added: "#0f0", deleted: "#f00", muted: "#888" };

describe("tree git status", () => {
  test("daemon statuses map onto the ones the tree draws", () => {
    expect(treeGitStatus("added")).toBe("added");
    expect(treeGitStatus("copied")).toBe("added");
    expect(treeGitStatus("untracked")).toBe("untracked");
    expect(treeGitStatus("renamed")).toBe("renamed");
    expect(treeGitStatus("deleted")).toBe("deleted");
    for (const edit of ["modified", "changed", "conflict", "anything-else"]) expect(treeGitStatus(edit)).toBe("modified");
  });
});

describe("tree row decoration", () => {
  test("counts show plus then minus, each in its own colour", () => {
    expect(decorationParts({ additions: 12, deletions: 3 }, colors)).toEqual([
      { text: "+12", color: "#0f0" },
      { text: " −3", color: "#f00" },
    ]);
    expect(decorationParts({ additions: 0, deletions: 3 }, colors)).toEqual([{ text: "−3", color: "#f00" }]);
    expect(decorationParts({ additions: 5, deletions: 0 }, colors)).toEqual([{ text: "+5", color: "#0f0" }]);
  });

  test("binary files say so, and rows with nothing counted stay clean", () => {
    expect(decorationParts({ additions: 0, deletions: 0, binary: true }, colors)).toEqual([{ text: "Binary", color: "#888" }]);
    expect(decorationParts({ additions: 0, deletions: 0 }, colors)).toBeNull();
    expect(decorationParts(undefined, colors)).toBeNull();
  });
});

describe("long press", () => {
  test("a finger inside the slop is still pressing, one past it is scrolling", () => {
    expect(movedPastSlop(0, 0, LONG_PRESS_SLOP_PX - 1, 0)).toBe(false);
    expect(movedPastSlop(0, 0, 0, LONG_PRESS_SLOP_PX + 1)).toBe(true);
    expect(movedPastSlop(10, 10, 14, 14)).toBe(false);
  });
});
