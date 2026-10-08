// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  brainWorkspaceEntryAccessibilityLabel,
  brainWorkspaceEntryIconName,
  brainWorkspaceMarkdownPath,
  switchExecutorAccessibilityLabel,
} from "./brainPresentation";

describe("brainWorkspaceMarkdownPath", () => {
  test("detects common markdown extensions", () => {
    expect(brainWorkspaceMarkdownPath("notes.md")).toBe(true);
    expect(brainWorkspaceMarkdownPath("NOTES.MD")).toBe(true);
    expect(brainWorkspaceMarkdownPath("readme.markdown")).toBe(true);
  });

  test("leaves non-markdown paths unmarked", () => {
    expect(brainWorkspaceMarkdownPath("LICENSE")).toBe(false);
    expect(brainWorkspaceMarkdownPath("notes.txt")).toBe(false);
    expect(brainWorkspaceMarkdownPath(".env")).toBe(false);
  });
});

describe("brainWorkspaceEntryAccessibilityLabel", () => {
  test("keeps folder and file semantics without visible type labels", () => {
    expect(brainWorkspaceEntryAccessibilityLabel("directory", "agents")).toBe(
      "Open folder agents",
    );
    expect(brainWorkspaceEntryAccessibilityLabel("file", "AGENTS.md")).toBe(
      "Open file AGENTS.md",
    );
    expect(
      brainWorkspaceEntryAccessibilityLabel(
        "file",
        "very-long-name-without-extension",
      ),
    ).toBe("Open file very-long-name-without-extension");
  });
});

describe("brainWorkspaceEntryIconName", () => {
  test("uses folder and document icons without restating type labels", () => {
    expect(brainWorkspaceEntryIconName("directory", "agents")).toBe(
      "folder",
    );
    expect(brainWorkspaceEntryIconName("directory", ".hidden")).toBe(
      "folder",
    );
    expect(brainWorkspaceEntryIconName("file", "AGENTS.md")).toBe(
      "document-text",
    );
    expect(brainWorkspaceEntryIconName("file", "LICENSE")).toBe(
      "document",
    );
  });

  test("marks audio and video as playable", () => {
    expect(brainWorkspaceEntryIconName("file", "promo/web.mp4")).toBe("play");
    expect(brainWorkspaceEntryIconName("file", "notes/memo.m4a")).toBe("play");
    expect(brainWorkspaceEntryIconName("file", "notes/memo.mp4.md")).toBe(
      "document-text",
    );
  });
});

describe("switchExecutorAccessibilityLabel", () => {
  test("names the Brain host", () => {
    expect(
      switchExecutorAccessibilityLabel({
        id: "claude",
        name: "Claude Code",
        provider: "claude",
      }),
    ).toBe("Switch Brain host, Claude Code");
    expect(switchExecutorAccessibilityLabel(null)).toBe(
      "Switch Brain host, unavailable",
    );
  });
});
