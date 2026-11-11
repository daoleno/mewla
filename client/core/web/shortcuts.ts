import type { DesktopPlaceKey } from "./places";

/**
 * The desktop keyboard model, pure so it is tested without a browser. The
 * shell and the composer read keys through these functions only. NSH's keymap
 * delivers a keystroke (`"g"`, `"shift+/"`, `"mod+k"`); `keyOfKeystroke` turns
 * it into the key these functions take.
 */

export interface KeyLike {
  key: string;
  metaKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
  isComposing?: boolean;
  keyCode?: number;
}

/**
 * An IME is composing (Chinese, Japanese, Korean input). Safari commits a
 * composition with isComposing false and keyCode 229, so both are checked.
 */
export function isImeComposing(event: KeyLike): boolean {
  return event.isComposing === true || event.keyCode === 229;
}

function hasMod(event: KeyLike): boolean {
  return Boolean(event.metaKey || event.ctrlKey);
}

export type ComposerKeyIntent = "send" | "hold" | "blur" | "recall" | null;

/**
 * What a key does in a chat composer. `desktop` means the primary pointer is
 * not a touchscreen: there Enter sends; elsewhere Enter keeps its newline.
 * Ctrl/⌘+Enter always sends, Shift+Enter is always a newline, and nothing
 * acts while an IME is composing. ↑ in an empty composer recalls the last
 * message wherever the key arrives (a phone's hardware keyboard too: a soft
 * keyboard has no ↑). While a slash-command list is open, a desktop Enter is
 * held (no send, no newline) so a half-typed `/co` never goes to the agent.
 */
export function composerKeyIntent(
  event: KeyLike,
  context: { desktop: boolean; draftEmpty: boolean; pickerOpen?: boolean },
): ComposerKeyIntent {
  if (isImeComposing(event)) return null;
  switch (event.key) {
    case "Enter":
      if (event.shiftKey || event.altKey) return null;
      if (context.pickerOpen) return context.desktop || hasMod(event) ? "hold" : null;
      if (hasMod(event)) return "send";
      return context.desktop ? "send" : null;
    case "Escape":
      return "blur";
    case "ArrowUp":
      if (!context.draftEmpty) return null;
      if (hasMod(event) || event.altKey || event.shiftKey) return null;
      return "recall";
    default:
      return null;
  }
}

/** Where a key went: a field, an editable region, or the terminal (which owns every key). */
export interface KeyTarget {
  tagName?: string;
  isContentEditable?: boolean;
  inTerminal?: boolean;
}

/** Typing in a field, an editable region or the terminal. */
export function isTypingTarget(target: KeyTarget | null | undefined): boolean {
  if (!target) return false;
  if (target.inTerminal || target.isContentEditable) return true;
  const tag = target.tagName?.toUpperCase();
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

export type DesktopCommand =
  | { kind: "go"; key: DesktopPlaceKey }
  | { kind: "palette" }
  | { kind: "shortcuts" }
  | { kind: "new-session" }
  | { kind: "focus-composer" };

/** `G` then a letter goes to a place, as in Linear and GitHub. */
export const DESKTOP_GO_KEYS: Readonly<Record<string, DesktopPlaceKey>> = {
  b: "brain",
  s: "sessions",
  c: "calendar",
  e: "extensions",
  t: "stats",
  r: "resources",
  ",": "settings",
};

const GO_LETTER: Partial<Record<DesktopPlaceKey, string>> = Object.fromEntries(
  Object.entries(DESKTOP_GO_KEYS).map(([letter, key]) => [key, letter]),
);

export interface GlobalKeyResult {
  command: DesktopCommand | null;
  /** The next key completes a `G` sequence. */
  pendingGo: boolean;
}

/**
 * A key anywhere on the page. Ctrl/⌘+K opens the palette from a field too,
 * but never from the terminal, where Ctrl+K is the shell's kill-line. Every
 * other shortcut is a bare key and stays out of fields entirely.
 */
export function resolveGlobalKey(
  event: KeyLike,
  state: { pendingGo: boolean; target: KeyTarget | null | undefined },
): GlobalKeyResult {
  const none = { command: null, pendingGo: false };
  if (isImeComposing(event)) return { command: null, pendingGo: state.pendingGo };
  const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
  if (hasMod(event) && !event.altKey && !event.shiftKey && key === "k") {
    return state.target?.inTerminal ? none : { command: { kind: "palette" }, pendingGo: false };
  }
  if (isTypingTarget(state.target)) return none;
  if (hasMod(event) || event.altKey) return none;
  if (state.pendingGo) {
    const place = DESKTOP_GO_KEYS[key];
    return place ? { command: { kind: "go", key: place }, pendingGo: false } : none;
  }
  if (event.key === "?") return { command: { kind: "shortcuts" }, pendingGo: false };
  if (event.shiftKey) return none;
  switch (key) {
    case "g":
      return { command: null, pendingGo: true };
    case "n":
      return { command: { kind: "new-session" }, pendingGo: false };
    case "/":
      return { command: { kind: "focus-composer" }, pendingGo: false };
    default:
      return none;
  }
}

/** The keystrokes (as `nsh/keymap` names them) the shell binds at all times, outside fields. */
export const GLOBAL_KEYSTROKES = ["shift+/", "g", "n", "/"] as const;
/** Bound only between `G` and the next key. */
export const GO_KEYSTROKES = Object.keys(DESKTOP_GO_KEYS);
/** How long `G` waits for its letter. */
export const GO_SEQUENCE_MS = 1500;

/**
 * A keystroke as `nsh/keymap` names it (`"mod+k"`, `"shift+/"`), as the key
 * event it stands for on the US layout. `mod` is Ctrl here: only "some
 * modifier" matters to these functions.
 */
export function keyOfKeystroke(keystroke: string): KeyLike {
  const parts = keystroke.toLowerCase().split("+");
  // `mod+plus` names `=`; a lone `+` would split to two empty parts.
  const last = parts.pop() || "+";
  const mods = new Set(parts);
  const shiftKey = mods.has("shift");
  const named: Record<string, string> = { enter: "Enter", escape: "Escape", up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight", space: " ", tab: "Tab", plus: "=", minus: "-" };
  let key = named[last] ?? last;
  if (shiftKey && key === "/") key = "?";
  return {
    key,
    shiftKey,
    altKey: mods.has("alt") || mods.has("option") || mods.has("opt"),
    ctrlKey: mods.has("ctrl") || mods.has("control") || mods.has("mod") || mods.has("cmdorctrl"),
    metaKey: mods.has("cmd") || mods.has("command") || mods.has("meta") || mods.has("super") || mods.has("win"),
  };
}

/** Shown in the cheat sheet, the palette and tooltips; none for a place without a letter. */
export function goShortcutLabel(key: DesktopPlaceKey): string | undefined {
  const letter = GO_LETTER[key];
  if (!letter) return undefined;
  return letter === "," ? "G then ," : `G then ${letter.toUpperCase()}`;
}

/** ⌘ on Apple platforms, Ctrl elsewhere. */
export function modKeyLabel(apple: boolean): string {
  return apple ? "⌘" : "Ctrl";
}

export interface ShortcutRow {
  keys: string;
  label: string;
}

export function desktopShortcutRows(mod: string): ShortcutRow[] {
  const go = (key: DesktopPlaceKey, label: string): ShortcutRow => ({ keys: goShortcutLabel(key) ?? "", label });
  return [
    { keys: `${mod}+K`, label: "Command palette" },
    { keys: "?", label: "Keyboard shortcuts" },
    { keys: "N", label: "New session" },
    { keys: "/", label: "Focus the message box" },
    go("brain", "Brain"),
    go("sessions", "Sessions"),
    go("calendar", "Calendar"),
    go("extensions", "Extensions"),
    go("stats", "Stats"),
    go("resources", "Resources"),
    go("settings", "Settings"),
    { keys: "Enter", label: "Send" },
    { keys: "Shift+Enter", label: "New line" },
    { keys: `${mod}+Enter`, label: "Send, always" },
    { keys: "↑", label: "Edit your last message (empty box)" },
    { keys: "Esc", label: "Close, or leave the message box" },
  ];
}

/** A sidebar row's tooltip: its name and how to get there. */
export function desktopShortcutTooltip(label: string, key: DesktopPlaceKey): string | undefined {
  const shortcut = goShortcutLabel(key);
  return shortcut ? `${label} (${shortcut})` : undefined;
}
