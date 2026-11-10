import { describe, expect, test } from "bun:test";
import {
  brainWorkspaceEntryAccessibilityLabel,
  brainWorkspaceEntryIconName,
  brainWorkspaceMarkdownPath,
  normalizeBrainWorkspaceTree,
  workspaceFolder,
  workspaceLinkPath,
} from "./workspace";

describe("Brain workspace", () => {
  test("a Markdown file is told by its extension", () => {
    expect(brainWorkspaceMarkdownPath("notes/Plan.MD")).toBe(true);
    expect(brainWorkspaceMarkdownPath("notes/plan.markdown")).toBe(true);
    expect(brainWorkspaceMarkdownPath("notes/plan.txt")).toBe(false);
  });

  test("rows say what tapping opens, and show folders, media and notes apart", () => {
    expect(brainWorkspaceEntryAccessibilityLabel("directory", "notes")).toBe("Open folder notes");
    expect(brainWorkspaceEntryAccessibilityLabel("file", "plan.md")).toBe("Open file plan.md");
    expect(brainWorkspaceEntryIconName("directory", "notes")).toBe("folder");
    expect(brainWorkspaceEntryIconName("file", "clips/demo.mp4")).toBe("play");
    expect(brainWorkspaceEntryIconName("file", "song.flac")).toBe("play");
    expect(brainWorkspaceEntryIconName("file", "plan.md")).toBe("document-text");
    expect(brainWorkspaceEntryIconName("file", "data.csv")).toBe("document");
  });

  test("folders come first, nameless entries are dropped, and a trail walks into a folder", () => {
    const tree = normalizeBrainWorkspaceTree({
      entries: [
        { name: "b.md", path: "b.md", kind: "file" },
        { name: "", path: "ghost", kind: "file" },
        { name: "notes", path: "notes", kind: "directory", children: [{ name: "z.md", path: "notes/z.md" }, { name: "a.md", path: "notes/a.md" }] },
        { name: "a.md", path: "a.md", kind: "file" },
      ],
    });
    expect(workspaceFolder(tree, []).map((entry) => entry.name)).toEqual(["notes", "a.md", "b.md"]);
    expect(workspaceFolder(tree, ["notes"]).map((entry) => entry.name)).toEqual(["a.md", "z.md"]);
    expect(workspaceFolder(tree, ["gone"])).toEqual([]);
  });

  test("a link in a note opens another workspace file, never one outside it", () => {
    expect(workspaceLinkPath("notes/plan.md", "./ideas.md")).toBe("notes/ideas.md");
    expect(workspaceLinkPath("notes/plan.md", "../todo.md#today")).toBe("todo.md");
    expect(workspaceLinkPath("notes/plan.md", "my%20file.md")).toBe("notes/my file.md");
    expect(workspaceLinkPath("plan.md", "../../etc/passwd")).toBeNull();
    expect(workspaceLinkPath("plan.md", "/etc/passwd")).toBeNull();
    expect(workspaceLinkPath("plan.md", "~/secrets.md")).toBeNull();
    expect(workspaceLinkPath("plan.md", "https://example.com/a.md")).toBeNull();
  });
});
