import { describe, expect, test } from "bun:test";
import {
  brainWorkspaceEntryAccessibilityLabel,
  brainWorkspaceEntryIconName,
  brainWorkspaceMarkdownPath,
  normalizeBrainWorkspaceTree,
  workspaceFolderEntries,
  workspaceFolderKey,
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

  test("folders come first and nameless entries are dropped; a folder is its own answer, without children", () => {
    // What the daemon sends for `brain_workspace_tree {path: ""}`: one level, `children` never filled.
    const root = normalizeBrainWorkspaceTree({
      workspace: "/home/me/brain",
      entries: [
        { name: "b.md", path: "b.md", kind: "file" },
        { name: "", path: "ghost", kind: "file" },
        { name: "notes", path: "notes", kind: "directory" },
        { name: "a.md", path: "a.md", kind: "file" },
      ],
    });
    expect(workspaceFolderEntries(root).map((entry) => entry.name)).toEqual(["notes", "a.md", "b.md"]);
    expect(root.entries.find((entry) => entry.name === "notes")?.children).toEqual([]);
    // `brain_workspace_tree {path: "notes"}`.
    const notes = normalizeBrainWorkspaceTree({ path: "notes", entries: [{ name: "z.md", path: "notes/z.md" }, { name: "a.md", path: "notes/a.md" }] });
    expect(workspaceFolderEntries(notes).map((entry) => entry.path)).toEqual(["notes/a.md", "notes/z.md"]);
    expect(workspaceFolderKey("s1", "notes")).not.toBe(workspaceFolderKey("s2", "notes"));
    expect(workspaceFolderKey("s1", "")).not.toBe(workspaceFolderKey("s1", "notes"));
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
