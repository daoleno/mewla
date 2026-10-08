import type { DesktopSidebarKey } from "./desktopWeb";

/**
 * Desktop web keyboard model, pure so it is tested without a browser. The
 * shell and the composer read keys through these functions only.
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
 * not a touchscreen: there Enter sends; elsewhere Enter keeps its newline. Ctrl/⌘+Enter always
 * sends, Shift+Enter is always the browser's newline, and nothing acts while
 * an IME is composing. ↑ in an empty composer recalls the last message.
 * While a slash-command or @mention list is open, a desktop Enter is held
 * (no send, no newline) so a half-typed `/co` never goes to the agent.
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
      if (!context.desktop || !context.draftEmpty) return null;
      if (hasMod(event) || event.altKey || event.shiftKey) return null;
      return "recall";
    default:
      return null;
  }
}

export interface KeyTarget {
  tagName?: string;
  isContentEditable?: boolean;
  /** True inside the terminal grid (xterm), which owns every key. */
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
  | { kind: "go"; key: DesktopSidebarKey }
  | { kind: "palette" }
  | { kind: "shortcuts" }
  | { kind: "new-session" }
  | { kind: "focus-composer" };

/** `G` then a letter goes to a place, as in Linear and GitHub. */
export const DESKTOP_GO_KEYS: Readonly<Record<string, DesktopSidebarKey>> = {
  b: "brain",
  s: "sessions",
  c: "calendar",
  p: "plugins",
  k: "skills",
  t: "stats",
  r: "resources",
  ",": "settings",
};

const GO_LETTER: Record<DesktopSidebarKey, string> = Object.fromEntries(
  Object.entries(DESKTOP_GO_KEYS).map(([letter, key]) => [key, letter]),
) as Record<DesktopSidebarKey, string>;

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

/** Shown in the cheat sheet, the palette and tooltips. */
export function goShortcutLabel(key: DesktopSidebarKey): string {
  const letter = GO_LETTER[key];
  return letter === "," ? "G then ," : `G then ${letter.toUpperCase()}`;
}

/** Ctrl on Windows and Linux, ⌘ on Apple platforms. */
export function modKeyLabel(platform: string | undefined): string {
  return platform && /mac|iphone|ipad/i.test(platform) ? "⌘" : "Ctrl";
}

export interface ShortcutRow {
  keys: string;
  label: string;
}

export function desktopShortcutRows(mod: string): ShortcutRow[] {
  return [
    { keys: `${mod}+K`, label: "Command palette" },
    { keys: "?", label: "Keyboard shortcuts" },
    { keys: "N", label: "New session" },
    { keys: "/", label: "Focus the message box" },
    { keys: goShortcutLabel("brain"), label: "Brain" },
    { keys: goShortcutLabel("sessions"), label: "Sessions" },
    { keys: goShortcutLabel("calendar"), label: "Calendar" },
    { keys: goShortcutLabel("plugins"), label: "Plugins" },
    { keys: goShortcutLabel("skills"), label: "Skills" },
    { keys: goShortcutLabel("stats"), label: "Stats" },
    { keys: goShortcutLabel("resources"), label: "Resources" },
    { keys: goShortcutLabel("settings"), label: "Settings" },
    { keys: "Enter", label: "Send" },
    { keys: "Shift+Enter", label: "New line" },
    { keys: `${mod}+Enter`, label: "Send, always" },
    { keys: "↑", label: "Edit your last message (empty box)" },
    { keys: "Esc", label: "Close, or leave the message box" },
  ];
}

/** A sidebar row's tooltip: its name and how to get there. */
export function desktopShortcutTooltip(label: string, key: string): string | undefined {
  return key in GO_LETTER ? `${label} (${goShortcutLabel(key as DesktopSidebarKey)})` : undefined;
}
