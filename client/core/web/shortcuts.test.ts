import { describe, expect, test } from "bun:test";
import {
  composerKeyIntent,
  desktopShortcutRows,
  desktopShortcutTooltip,
  goShortcutLabel,
  GLOBAL_KEYSTROKES,
  GO_KEYSTROKES,
  keyOfKeystroke,
  modKeyLabel,
  resolveGlobalKey,
} from "./shortcuts";

const desktop = { desktop: true, draftEmpty: false };

describe("composer keys", () => {
  test("Enter sends on a desktop and keeps its newline on a touchscreen", () => {
    expect(composerKeyIntent({ key: "Enter" }, desktop)).toBe("send");
    expect(composerKeyIntent({ key: "Enter" }, { ...desktop, desktop: false })).toBeNull();
  });

  test("Shift+Enter and Alt+Enter are newlines; Ctrl/⌘+Enter always sends", () => {
    expect(composerKeyIntent({ key: "Enter", shiftKey: true }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "Enter", altKey: true }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "Enter", ctrlKey: true }, { ...desktop, desktop: false })).toBe("send");
    expect(composerKeyIntent({ key: "Enter", metaKey: true }, desktop)).toBe("send");
  });

  test("nothing acts while an IME composes, Safari's keyCode 229 included", () => {
    expect(composerKeyIntent({ key: "Enter", isComposing: true }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "Enter", keyCode: 229 }, desktop)).toBeNull();
    expect(composerKeyIntent({ key: "ArrowUp", isComposing: true }, { ...desktop, draftEmpty: true })).toBeNull();
  });

  test("an open slash list holds Enter instead of sending a half-typed command", () => {
    expect(composerKeyIntent({ key: "Enter" }, { ...desktop, pickerOpen: true })).toBe("hold");
    expect(composerKeyIntent({ key: "Enter", ctrlKey: true }, { desktop: false, draftEmpty: false, pickerOpen: true })).toBe("hold");
    expect(composerKeyIntent({ key: "Enter" }, { desktop: false, draftEmpty: false, pickerOpen: true })).toBeNull();
  });

  test("↑ recalls only in an empty composer, bare, a phone's hardware keyboard too; Esc blurs", () => {
    expect(composerKeyIntent({ key: "ArrowUp" }, { desktop: true, draftEmpty: true })).toBe("recall");
    expect(composerKeyIntent({ key: "ArrowUp" }, { desktop: true, draftEmpty: false })).toBeNull();
    expect(composerKeyIntent({ key: "ArrowUp" }, { desktop: false, draftEmpty: true })).toBe("recall");
    expect(composerKeyIntent({ key: "ArrowUp", shiftKey: true }, { desktop: true, draftEmpty: true })).toBeNull();
    expect(composerKeyIntent({ key: "Escape" }, desktop)).toBe("blur");
  });
});

describe("global keys", () => {
  const page = { pendingGo: false, target: { tagName: "DIV" } };

  test("? N / and G then a letter, outside fields", () => {
    expect(resolveGlobalKey({ key: "?", shiftKey: true }, page).command).toEqual({ kind: "shortcuts" });
    expect(resolveGlobalKey({ key: "n" }, page).command).toEqual({ kind: "new-session" });
    expect(resolveGlobalKey({ key: "/" }, page).command).toEqual({ kind: "focus-composer" });
    const g = resolveGlobalKey({ key: "g" }, page);
    expect(g).toEqual({ command: null, pendingGo: true });
    expect(resolveGlobalKey({ key: "c" }, { ...page, pendingGo: true }).command).toEqual({ kind: "go", key: "calendar" });
    expect(resolveGlobalKey({ key: "e" }, { ...page, pendingGo: true }).command).toEqual({ kind: "go", key: "extensions" });
    expect(resolveGlobalKey({ key: "," }, { ...page, pendingGo: true }).command).toEqual({ kind: "go", key: "settings" });
    expect(resolveGlobalKey({ key: "x" }, { ...page, pendingGo: true })).toEqual({ command: null, pendingGo: false });
  });

  test("never while typing in a field, an editable region or the terminal", () => {
    for (const target of [{ tagName: "input" }, { tagName: "TEXTAREA" }, { isContentEditable: true }, { inTerminal: true }]) {
      expect(resolveGlobalKey({ key: "n" }, { pendingGo: false, target }).command).toBeNull();
      expect(resolveGlobalKey({ key: "c" }, { pendingGo: true, target }).command).toBeNull();
    }
  });

  test("Ctrl/⌘+K opens the palette from a field too, never from the terminal", () => {
    expect(resolveGlobalKey({ key: "k", ctrlKey: true }, { pendingGo: false, target: { tagName: "TEXTAREA" } }).command).toEqual({ kind: "palette" });
    expect(resolveGlobalKey({ key: "k", metaKey: true }, page).command).toEqual({ kind: "palette" });
    expect(resolveGlobalKey({ key: "k", ctrlKey: true }, { pendingGo: false, target: { inTerminal: true } }).command).toBeNull();
    expect(resolveGlobalKey({ key: "k", ctrlKey: true, shiftKey: true }, page).command).toBeNull();
  });

  test("a modifier or a composition is never a bare shortcut", () => {
    expect(resolveGlobalKey({ key: "n", ctrlKey: true }, page).command).toBeNull();
    expect(resolveGlobalKey({ key: "n", altKey: true }, page).command).toBeNull();
    expect(resolveGlobalKey({ key: "g", isComposing: true }, { ...page, pendingGo: true })).toEqual({ command: null, pendingGo: true });
  });

  test("the keystrokes the shell binds mean what the resolver expects", () => {
    expect(GLOBAL_KEYSTROKES.map((keystroke) => resolveGlobalKey(keyOfKeystroke(keystroke), page).command?.kind ?? "pending")).toEqual(["shortcuts", "pending", "new-session", "focus-composer"]);
    for (const keystroke of GO_KEYSTROKES) expect(resolveGlobalKey(keyOfKeystroke(keystroke), { ...page, pendingGo: true }).command?.kind).toBe("go");
    expect(resolveGlobalKey(keyOfKeystroke("mod+k"), page).command).toEqual({ kind: "palette" });
    expect(keyOfKeystroke("shift+enter")).toMatchObject({ key: "Enter", shiftKey: true });
    expect(keyOfKeystroke("up")).toMatchObject({ key: "ArrowUp", shiftKey: false, ctrlKey: false });
  });
});

describe("shortcut labels", () => {
  test("G letters for the places that have one", () => {
    expect(goShortcutLabel("calendar")).toBe("G then C");
    expect(goShortcutLabel("settings")).toBe("G then ,");
    expect(goShortcutLabel("screen")).toBeUndefined();
    expect(desktopShortcutTooltip("Stats", "stats")).toBe("Stats (G then T)");
    expect(desktopShortcutTooltip("Friends", "friends")).toBeUndefined();
  });

  test("the cheat sheet names the platform's modifier", () => {
    expect(modKeyLabel(true)).toBe("⌘");
    expect(modKeyLabel(false)).toBe("Ctrl");
    const rows = desktopShortcutRows("Ctrl");
    expect(rows[0]).toEqual({ keys: "Ctrl+K", label: "Command palette" });
    expect(rows.find((row) => row.label === "Extensions")?.keys).toBe("G then E");
    expect(rows.every((row) => row.keys.length > 0)).toBe(true);
  });
});
