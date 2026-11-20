import { describe, expect, test } from "bun:test";
import { heldBy, knownHolder, normalise, shortcutOf } from "./shortcut";

const none = { control: false, alt: false, shift: false, platform: false };
const heard = (code: string, key: string, mods: Partial<typeof none> = {}) => ({ code, key, modifiers: { ...none, ...mods } });

describe("the quick chat's recorder", () => {
  test("a key with ⌘, ⌃ or ⌥ is a shortcut, spelt as the keymap spells it", () => {
    expect(shortcutOf(heard("Space", " ", { platform: true, shift: true }))).toBe("shift+mod+space");
    expect(shortcutOf(heard("KeyK", "k", { alt: true }))).toBe("alt+k");
    expect(shortcutOf(heard("Digit1", "!", { platform: true, shift: true }))).toBe("shift+mod+1");
    // The physical key, whatever the layout or Option types.
    expect(shortcutOf(heard("KeyM", "µ", { alt: true }))).toBe("alt+m");
    expect(shortcutOf(heard("F5", "F5"))).toBe("f5");
  });
  test("not yet: a modifier alone, a plain or Shift-only key, Escape", () => {
    expect(shortcutOf(heard("ShiftLeft", "Shift", { shift: true }))).toBeNull();
    expect(shortcutOf(heard("MetaLeft", "Meta", { platform: true }))).toBeNull();
    expect(shortcutOf(heard("KeyA", "A", { shift: true }))).toBeNull();
    expect(shortcutOf(heard("Escape", "Escape", { platform: true }))).toBeNull();
  });
  test("without a code (synthetic input) the key is read", () => {
    expect(shortcutOf(heard("", " ", { platform: true }))).toBe("mod+space");
    expect(shortcutOf(heard("", "Unidentified", { platform: true }))).toBeNull();
  });
  test("one spelling per keystroke; the apps that hold the usual ones are named", () => {
    expect(normalise("mod+shift+space")).toBe("shift+mod+space");
    expect(normalise("shift+mod+space")).toBe("shift+mod+space");
    expect(heldBy("mod+space")).toBe("Spotlight or Raycast");
    expect(heldBy("alt+space")).toBe("ChatGPT");
    expect(heldBy("shift+mod+k")).toBe("another app");
  });
  test("what a Mac already uses is never taken: the system's, every app's Copy, Quit and Close, the launchers", () => {
    for (const taken of ["mod+c", "mod+v", "mod+q", "mod+w", "mod+z", "shift+mod+z", "mod+tab", "mod+space", "alt+space", "shift+mod+4"]) {
      expect(knownHolder(taken)).not.toBeNull();
    }
    expect(knownHolder("mod+c")).toBe("every app (Edit and File menus)");
    expect(knownHolder("mod+shift+space")).toBeNull();
    expect(knownHolder("alt+mod+k")).toBeNull();
  });
});
