/**
 * The quick chat's shortcut (DESIGN-MAC.md §2.4, `m-shortcut`): a keystroke the recorder heard, as the keymap spells
 * it (`"mod+shift+space"`), or null while it is not a shortcut yet (a modifier alone, or a key with no ⌘, ⌃ or ⌥).
 */
export type HeardKey = {
  key: string;
  code: string;
  modifiers: { control: boolean; alt: boolean; shift: boolean; platform: boolean };
};

const MODIFIER_CODES = /^(Shift|Control|Alt|Meta|OS|CapsLock|Fn)(Left|Right)?$/;
const NAMED: Record<string, string> = {
  Space: "space",
  Enter: "enter",
  Tab: "tab",
  Backspace: "backspace",
  Delete: "delete",
  ArrowUp: "up",
  ArrowDown: "down",
  ArrowLeft: "left",
  ArrowRight: "right",
  Home: "home",
  End: "end",
  PageUp: "pageup",
  PageDown: "pagedown",
  Comma: ",",
  Period: ".",
  Slash: "/",
  Semicolon: ";",
  Quote: "'",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Minus: "-",
  Equal: "=",
  Backquote: "`",
};

/** The key itself, from its physical code where there is one (layout and Shift do not change it), else its key. */
function keyName(heard: HeardKey): string | null {
  const code = heard.code;
  if (code) {
    if (MODIFIER_CODES.test(code)) return null;
    const letter = /^Key([A-Z])$/.exec(code);
    if (letter) return letter[1].toLowerCase();
    const digit = /^Digit([0-9])$/.exec(code);
    if (digit) return digit[1];
    if (/^F([1-9]|1[0-9])$/.test(code)) return code.toLowerCase();
    if (NAMED[code]) return NAMED[code];
  }
  const key = heard.key;
  if (!key || key === "Unidentified" || /^(Shift|Control|Alt|Meta|OS|CapsLock|Fn)$/.test(key)) return null;
  if (key === " ") return "space";
  if (NAMED[key]) return NAMED[key];
  return key.length === 1 ? key.toLowerCase() : null;
}

/** The keymap's spelling of a heard keystroke, or null when it is not a shortcut a whole Mac can share. */
export function shortcutOf(heard: HeardKey): string | null {
  const key = keyName(heard);
  const m = heard.modifiers;
  if (!key) return null;
  // Escape cancels the recorder; a key needs ⌘, ⌃ or ⌥ (Shift alone would take a letter from every text field).
  if (heard.key === "Escape" || heard.code === "Escape") return null;
  if (!m.platform && !m.control && !m.alt && !/^f([1-9]|1[0-9])$/.test(key)) return null;
  return [m.control && "ctrl", m.alt && "alt", m.shift && "shift", m.platform && "mod", key].filter(Boolean).join("+");
}

/**
 * Shortcuts a Mac already uses (§2.4): the system's, every app's own (Copy, Quit, Close…, which a global shortcut
 * would take from all of them, as the system does not refuse them), and the agents' launchers. Refused before the
 * system is asked, and named when it refuses another.
 */
const EVERY_APP = "every app (Edit and File menus)";
const KNOWN_HOLDERS: Record<string, string> = {
  "mod+space": "Spotlight or Raycast",
  "alt+space": "ChatGPT",
  "ctrl+space": "Input Sources",
  "ctrl+mod+space": "the Character Viewer",
  "mod+tab": "the app switcher",
  "mod+`": "the window switcher",
  "alt+mod+escape": "Force Quit",
  "ctrl+mod+q": "Lock Screen",
  "ctrl+mod+f": "full screen",
  "shift+mod+3": "Screenshot",
  "shift+mod+4": "Screenshot",
  "shift+mod+5": "Screenshot",
  "shift+mod+z": EVERY_APP,
  ...Object.fromEntries(["a", "c", "v", "x", "z", "s", "q", "w", "n", "o", "p", "f", "h", "m", "t", ","].map((key) => [`mod+${key}`, EVERY_APP])),
};

/** Who already uses `shortcut` on a Mac, or null when nothing known does. */
export function knownHolder(shortcut: string): string | null {
  return KNOWN_HOLDERS[normalise(shortcut)] ?? null;
}

/** Who holds `shortcut` when the system refuses it (a known app, or another app). */
export function heldBy(shortcut: string): string {
  return knownHolder(shortcut) ?? "another app";
}

/** One spelling per keystroke: the modifiers in the keymap's order. */
export function normalise(shortcut: string): string {
  const parts = shortcut.toLowerCase().split("+");
  const key = parts.pop() ?? "";
  const has = (name: string) => parts.includes(name);
  return [has("ctrl") && "ctrl", has("alt") && "alt", has("shift") && "shift", (has("mod") || has("cmd") || has("meta")) && "mod", key].filter(Boolean).join("+");
}
