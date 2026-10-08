import { describe, expect, test } from "bun:test";
import {
  composerKeyIntent,
  desktopShortcutTooltip,
  isImeComposing,
  isTypingTarget,
  modKeyLabel,
  resolveGlobalKey,
} from "./desktopShortcuts";

const desktop = { desktop: true, draftEmpty: false };
const phone = { desktop: false, draftEmpty: false };

describe("composer keys", () => {
  test("Enter sends on desktop and keeps its newline on phone", () => {
    expect(composerKeyIntent({ key: "Enter" }, desktop)).toBe("send");
    expect(composerKeyIntent({ key: "Enter" }, phone)).toBeNull();
  });

  test("Shift+Enter is always the browser's newline", () => {
    expect(composerKeyIntent({ key: "Enter", shiftKey: true }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "Enter", shiftKey: true, ctrlKey: true }, desktop)).toBeNull();
  });

  test("Ctrl/⌘+Enter always sends, phone included", () => {
    expect(composerKeyIntent({ key: "Enter", ctrlKey: true }, desktop)).toBe("send");
    expect(composerKeyIntent({ key: "Enter", metaKey: true }, desktop)).toBe("send");
    expect(composerKeyIntent({ key: "Enter", metaKey: true }, phone)).toBe("send");
  });

  test("nothing sends while an IME is composing", () => {
    // Chrome and Firefox: the Enter that picks a candidate reports isComposing.
    expect(composerKeyIntent({ key: "Enter", isComposing: true }, desktop)).toBeNull();
    // Safari commits with isComposing false but keyCode 229.
    expect(composerKeyIntent({ key: "Enter", keyCode: 229 }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "Enter", ctrlKey: true, isComposing: true }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "Escape", isComposing: true }, desktop)).toBeNull();
    expect(isImeComposing({ key: "Process", keyCode: 229 })).toBe(true);
    expect(isImeComposing({ key: "Enter", keyCode: 13 })).toBe(false);
  });

  test("Esc leaves the box; ↑ recalls only in an empty box on desktop", () => {
    expect(composerKeyIntent({ key: "Escape" }, desktop)).toBe("blur");
    expect(composerKeyIntent({ key: "ArrowUp" }, { desktop: true, draftEmpty: true })).toBe("recall");
    expect(composerKeyIntent({ key: "ArrowUp" }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "ArrowUp" }, { desktop: false, draftEmpty: true })).toBeNull();
    expect(composerKeyIntent({ key: "ArrowUp", shiftKey: true }, { desktop: true, draftEmpty: true })).toBeNull();
    expect(composerKeyIntent({ key: "a" }, desktop)).toBeNull();
  });
});

describe("global keys", () => {
  const body = { tagName: "BODY" };
  const field = { tagName: "TEXTAREA" };
  const terminal = { tagName: "TEXTAREA", inTerminal: true };

  test("typing targets", () => {
    expect(isTypingTarget(field)).toBe(true);
    expect(isTypingTarget({ tagName: "input" })).toBe(true);
    expect(isTypingTarget({ tagName: "DIV", isContentEditable: true })).toBe(true);
    expect(isTypingTarget({ tagName: "DIV", inTerminal: true })).toBe(true);
    expect(isTypingTarget(body)).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });

  test("Ctrl/⌘+K opens the palette, from a field too, never from the terminal", () => {
    expect(resolveGlobalKey({ key: "k", ctrlKey: true }, { pendingGo: false, target: body }).command).toEqual({ kind: "palette" });
    expect(resolveGlobalKey({ key: "K", metaKey: true }, { pendingGo: false, target: field }).command).toEqual({ kind: "palette" });
    expect(resolveGlobalKey({ key: "k", ctrlKey: true }, { pendingGo: false, target: terminal }).command).toBeNull();
    expect(resolveGlobalKey({ key: "k", ctrlKey: true, shiftKey: true }, { pendingGo: false, target: body }).command).toBeNull();
  });

  test("G then a letter goes to a place", () => {
    const first = resolveGlobalKey({ key: "g" }, { pendingGo: false, target: body });
    expect(first).toEqual({ command: null, pendingGo: true });
    expect(resolveGlobalKey({ key: "c" }, { pendingGo: true, target: body }).command).toEqual({ kind: "go", key: "calendar" });
    expect(resolveGlobalKey({ key: "," }, { pendingGo: true, target: body }).command).toEqual({ kind: "go", key: "settings" });
    expect(resolveGlobalKey({ key: "x" }, { pendingGo: true, target: body })).toEqual({ command: null, pendingGo: false });
  });

  test("bare keys never fire while typing or composing", () => {
    for (const key of ["g", "n", "/", "?"]) {
      expect(resolveGlobalKey({ key }, { pendingGo: false, target: field }).command).toBeNull();
      expect(resolveGlobalKey({ key }, { pendingGo: false, target: terminal }).command).toBeNull();
    }
    expect(resolveGlobalKey({ key: "n", isComposing: true }, { pendingGo: false, target: body }).command).toBeNull();
    expect(resolveGlobalKey({ key: "n", ctrlKey: true }, { pendingGo: false, target: body }).command).toBeNull();
  });

  test("?, N and /", () => {
    expect(resolveGlobalKey({ key: "?", shiftKey: true }, { pendingGo: false, target: body }).command).toEqual({ kind: "shortcuts" });
    expect(resolveGlobalKey({ key: "n" }, { pendingGo: false, target: body }).command).toEqual({ kind: "new-session" });
    expect(resolveGlobalKey({ key: "N", shiftKey: true }, { pendingGo: false, target: body }).command).toBeNull();
    expect(resolveGlobalKey({ key: "/" }, { pendingGo: false, target: body }).command).toEqual({ kind: "focus-composer" });
  });

  test("labels", () => {
    expect(modKeyLabel("MacIntel")).toBe("⌘");
    expect(modKeyLabel("Linux x86_64")).toBe("Ctrl");
    expect(desktopShortcutTooltip("Calendar", "calendar")).toBe("Calendar (G then C)");
    expect(desktopShortcutTooltip("Settings", "settings")).toBe("Settings (G then ,)");
    expect(desktopShortcutTooltip("Browser", "browser")).toBeUndefined();
  });
});
