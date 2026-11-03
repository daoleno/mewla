import { describe, expect, test } from "bun:test";
import {
  MAX_REMOTE_CLIPBOARD_CHARS,
  PENDING_CLIPBOARD_TTL_MS,
  TerminalClipboard,
  createProgramClipboardProvider,
  isMacPlatform,
  terminalKeyAction,
  type ClipboardEnvironment,
  type TerminalKeyEvent,
} from "./terminalClipboard";

function key(over: Partial<TerminalKeyEvent>): TerminalKeyEvent {
  return {
    type: "keydown",
    key: "c",
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    altKey: false,
    ...over,
  };
}

describe("terminalKeyAction", () => {
  test("Ctrl+C copies a selection and drops it, so the next Ctrl+C interrupts", () => {
    expect(terminalKeyAction(key({ ctrlKey: true }), true, false))
      .toEqual({ kind: "copy", clearSelection: true });
  });

  test("Ctrl+C with no selection stays ^C for the program", () => {
    expect(terminalKeyAction(key({ ctrlKey: true }), false, false)).toEqual({ kind: "pass" });
  });

  test("Ctrl+Shift+C copies a selection and keeps it", () => {
    expect(terminalKeyAction(key({ ctrlKey: true, shiftKey: true, key: "C" }), true, false))
      .toEqual({ kind: "copy", clearSelection: false });
  });

  test("Ctrl+Shift+C with no selection is swallowed, never sent as ^C", () => {
    expect(terminalKeyAction(key({ ctrlKey: true, shiftKey: true, key: "C" }), false, false))
      .toEqual({ kind: "swallow" });
  });

  test("Cmd+C copies a selection on a Mac and never interrupts", () => {
    expect(terminalKeyAction(key({ metaKey: true }), true, true))
      .toEqual({ kind: "copy", clearSelection: false });
    expect(terminalKeyAction(key({ metaKey: true }), false, true)).toEqual({ kind: "pass" });
  });

  test("Ctrl+C on a Mac is always ^C, even with a selection", () => {
    expect(terminalKeyAction(key({ ctrlKey: true }), true, true)).toEqual({ kind: "pass" });
  });

  test("Meta+C elsewhere and other keys are left alone", () => {
    expect(terminalKeyAction(key({ metaKey: true }), true, false)).toEqual({ kind: "pass" });
    expect(terminalKeyAction(key({ ctrlKey: true, key: "v" }), true, false)).toEqual({ kind: "pass" });
    expect(terminalKeyAction(key({ ctrlKey: true, altKey: true }), true, false)).toEqual({ kind: "pass" });
    expect(terminalKeyAction(key({ ctrlKey: true, type: "keyup" }), true, false)).toEqual({ kind: "pass" });
    expect(terminalKeyAction(key({}), true, false)).toEqual({ kind: "pass" });
  });
});

describe("isMacPlatform", () => {
  test("detects Mac from platform or user agent", () => {
    expect(isMacPlatform({ platform: "MacIntel" })).toBe(true);
    expect(isMacPlatform({ platform: "", userAgent: "Mozilla/5.0 (Macintosh)" })).toBe(true);
    expect(isMacPlatform({ platform: "Linux x86_64" })).toBe(false);
    expect(isMacPlatform({ platform: "Win32" })).toBe(false);
  });
});

function harness(over: Partial<ClipboardEnvironment> = {}) {
  const written: string[] = [];
  const state = { time: 1000, allow: true };
  const env: ClipboardEnvironment = {
    isSecureContext: () => true,
    isVisible: () => true,
    writeText: async (text) => {
      if (!state.allow) throw new Error("NotAllowedError");
      written.push(text);
    },
    now: () => state.time,
    ...over,
  };
  return { clipboard: new TerminalClipboard(env), written, state };
}

describe("TerminalClipboard", () => {
  test("copySelection writes the selection and ignores an empty one", async () => {
    const { clipboard, written } = harness();
    expect(await clipboard.copySelection("hello")).toBe(true);
    expect(await clipboard.copySelection("")).toBe(false);
    expect(written).toEqual(["hello"]);
  });

  test("copySelection falls back to the legacy copy when the async API refuses", async () => {
    const legacy: string[] = [];
    const { clipboard, state } = harness({ legacyCopy: (text) => (legacy.push(text), true) });
    state.allow = false;
    expect(await clipboard.copySelection("x")).toBe(true);
    expect(legacy).toEqual(["x"]);
  });

  test("a program's copy is written on a visible secure page", async () => {
    const { clipboard, written } = harness();
    expect(await clipboard.copyFromProgram("from tmux")).toBe(true);
    expect(written).toEqual(["from tmux"]);
  });

  test("a program's copy is dropped on an insecure origin or a hidden page", async () => {
    const insecure = harness({ isSecureContext: () => false });
    expect(await insecure.clipboard.copyFromProgram("a")).toBe(false);
    expect(insecure.clipboard.hasPending()).toBe(false);
    const hidden = harness({ isVisible: () => false });
    expect(await hidden.clipboard.copyFromProgram("a")).toBe(false);
    expect(hidden.written).toEqual([]);
  });

  test("a program's oversized or empty copy is dropped", async () => {
    const { clipboard, written } = harness();
    expect(await clipboard.copyFromProgram("")).toBe(false);
    expect(await clipboard.copyFromProgram("x".repeat(MAX_REMOTE_CLIPBOARD_CHARS + 1))).toBe(false);
    expect(written).toEqual([]);
  });

  test("a refused program copy waits for the next gesture and then lands", async () => {
    const { clipboard, written, state } = harness();
    state.allow = false;
    expect(await clipboard.copyFromProgram("held")).toBe(false);
    expect(clipboard.hasPending()).toBe(true);
    await clipboard.flushPending();
    expect(clipboard.hasPending()).toBe(true);
    state.allow = true;
    await clipboard.flushPending();
    expect(written).toEqual(["held"]);
    expect(clipboard.hasPending()).toBe(false);
  });

  test("a held program copy expires and never replaces a newer user copy", async () => {
    const { clipboard, written, state } = harness();
    state.allow = false;
    await clipboard.copyFromProgram("stale");
    state.allow = true;
    state.time += PENDING_CLIPBOARD_TTL_MS + 1;
    await clipboard.flushPending();
    expect(written).toEqual([]);

    state.allow = false;
    await clipboard.copyFromProgram("older");
    state.allow = true;
    await clipboard.copySelection("newer");
    await clipboard.flushPending();
    expect(written).toEqual(["newer"]);
  });
});

describe("OSC 52 provider", () => {
  test("never reads the clipboard for a program", () => {
    const { clipboard } = harness();
    expect(createProgramClipboardProvider(clipboard).readText()).toBe("");
  });

  test("writes only the system clipboard", async () => {
    const { clipboard, written } = harness();
    const provider = createProgramClipboardProvider(clipboard);
    await provider.writeText("c", "one");
    await provider.writeText("", "two");
    await provider.writeText("p", "three");
    await provider.writeText("0", "four");
    expect(written).toEqual(["one", "two"]);
  });
});
