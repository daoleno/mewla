// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { brainReducer, initialBrainState } from "../../store/brain";
import {
  brainExecutorChoices,
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

describe("brainExecutorChoices", () => {
  const executors = ["claude", "codex", "grok", "pi"].map((id) => ({ id, name: id }));
  const snapshot = (executor_states: unknown) =>
    brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "s1",
      serverName: "Mewla",
      serverUrl: "ws://mewla",
      brain: { executors, executor_states },
    } as any).byServer["s1"]!;

  test("offers only installed agents, signed-in ones first, and says more can be installed", () => {
    const brain = snapshot({
      claude: { installed: true },
      codex: { installed: true, signed_in: true },
      grok: { installed: false, signed_in: true },
    });
    const { choices, moreToInstall } = brainExecutorChoices(brain.executors!, brain.executor_states);
    expect(choices.map((item) => item.id)).toEqual(["codex", "claude"]);
    expect(moreToInstall).toBe(true);
  });

  test("lists nothing when no agent is installed, and no install line when all are", () => {
    const none = snapshot({});
    expect(brainExecutorChoices(none.executors!, none.executor_states)).toEqual({ choices: [], moreToInstall: true });
    // A computer that sends no states cannot say anything is installed.
    expect(brainExecutorChoices(executors, undefined).choices).toEqual([]);
    const all = snapshot(Object.fromEntries(executors.map((item) => [item.id, { installed: true }])));
    const result = brainExecutorChoices(all.executors!, all.executor_states);
    expect(result.choices.map((item) => item.id)).toEqual(["claude", "codex", "grok", "pi"]);
    expect(result.moreToInstall).toBe(false);
  });
});
