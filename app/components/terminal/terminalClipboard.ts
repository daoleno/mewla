/**
 * Clipboard rules for the web terminal: which key presses copy a selection
 * instead of reaching the shell, and when text a program asks for over OSC 52
 * (tmux copy-mode yank, vim, nvim, ...) may land in the browser's clipboard.
 */

export type TerminalKeyEvent = {
  type: string;
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
};

export type TerminalKeyAction =
  /** Copy the selection and keep the key away from the program. */
  | { kind: "copy"; clearSelection: boolean }
  /** Keep the key away from the program without copying. */
  | { kind: "swallow" }
  /** Let the terminal handle the key as usual. */
  | { kind: "pass" };

const PASS: TerminalKeyAction = { kind: "pass" };

/**
 * Cmd+C copies on a Mac. Elsewhere Ctrl+Shift+C always belongs to the
 * terminal, and plain Ctrl+C copies only while text is selected, so with
 * nothing selected it still interrupts the program (^C).
 */
export function terminalKeyAction(
  event: TerminalKeyEvent,
  hasSelection: boolean,
  mac: boolean,
): TerminalKeyAction {
  if (event.type !== "keydown" || event.key.toLowerCase() !== "c" || event.altKey) {
    return PASS;
  }
  if (mac) {
    if (!event.metaKey || event.ctrlKey || event.shiftKey) return PASS;
    return hasSelection ? { kind: "copy", clearSelection: false } : PASS;
  }
  if (!event.ctrlKey || event.metaKey) return PASS;
  if (event.shiftKey) {
    return hasSelection ? { kind: "copy", clearSelection: false } : { kind: "swallow" };
  }
  // A copy through plain Ctrl+C drops the selection, so the next Ctrl+C
  // interrupts again instead of copying the same text a second time.
  return hasSelection ? { kind: "copy", clearSelection: true } : PASS;
}

export function isMacPlatform(
  nav: { platform?: string; userAgent?: string } | undefined = typeof navigator === "undefined"
    ? undefined
    : navigator,
): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(nav?.platform || nav?.userAgent || "");
}

/** What a clipboard write may depend on; the browser supplies these. */
export type ClipboardEnvironment = {
  /** HTTPS, or a loopback address: the only origins that may write silently. */
  isSecureContext: () => boolean;
  /** The terminal's page is on screen, so a write is something the user can see. */
  isVisible: () => boolean;
  /** navigator.clipboard.writeText; absent where the browser has none. */
  writeText?: (text: string) => Promise<void>;
  /** Last resort for a copy the user just asked for (document.execCommand). */
  legacyCopy?: (text: string) => boolean;
  now: () => number;
};

// Programs ask for the clipboard with a few hundred bytes to a few hundred
// kilobytes; more than this is not a copy, it is a page trying to fill memory.
export const MAX_REMOTE_CLIPBOARD_CHARS = 1 << 20;
// A remote copy that the browser refused waits for the next click or key press;
// an old one must not replace what the user copied since.
export const PENDING_CLIPBOARD_TTL_MS = 10_000;

export class TerminalClipboard {
  private pending: { text: string; at: number } | null = null;

  constructor(private readonly env: ClipboardEnvironment) {}

  /** A copy the user asked for with the keyboard: it runs inside the key press. */
  async copySelection(text: string): Promise<boolean> {
    if (!text) return false;
    this.pending = null;
    if (await this.tryWrite(text)) return true;
    return this.env.legacyCopy?.(text) ?? false;
  }

  /**
   * Text a program put on the clipboard with OSC 52. Without a user gesture
   * some browsers (Safari) refuse the write, so it waits for the next click or
   * key press instead of being lost.
   */
  async copyFromProgram(text: string): Promise<boolean> {
    if (!text || text.length > MAX_REMOTE_CLIPBOARD_CHARS) return false;
    if (!this.env.isSecureContext() || !this.env.isVisible()) return false;
    if (await this.tryWrite(text)) {
      this.pending = null;
      return true;
    }
    this.pending = { text, at: this.env.now() };
    return false;
  }

  /** Called on a click or key press: finishes a remote copy the browser refused. */
  async flushPending(): Promise<void> {
    const pending = this.pending;
    if (!pending) return;
    this.pending = null;
    if (this.env.now() - pending.at > PENDING_CLIPBOARD_TTL_MS) return;
    if (!(await this.tryWrite(pending.text))) this.pending = pending;
  }

  hasPending(): boolean {
    return this.pending !== null;
  }

  private async tryWrite(text: string): Promise<boolean> {
    const write = this.env.writeText;
    if (!write) return false;
    try {
      await write(text);
      return true;
    } catch {
      return false;
    }
  }
}

/**
 * The provider @xterm/addon-clipboard calls for OSC 52. It only writes, only
 * the system clipboard, and never reads: a program in the pane must not be
 * able to ask the browser what the user copied elsewhere.
 */
export function createProgramClipboardProvider(clipboard: TerminalClipboard) {
  return {
    readText(): string {
      return "";
    },
    async writeText(selection: string, text: string): Promise<void> {
      // tmux sends an empty selection or "c"; "p" and the cut buffers are X11
      // selections that have no browser counterpart.
      if (selection !== "" && !selection.includes("c")) return;
      await clipboard.copyFromProgram(text);
    },
  };
}

function legacyCopy(text: string): boolean {
  const body = document.body;
  if (!body) return false;
  const active = document.activeElement as HTMLElement | null;
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
  body.appendChild(area);
  try {
    area.select();
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    body.removeChild(area);
    active?.focus?.();
  }
}

export function browserClipboardEnvironment(): ClipboardEnvironment {
  return {
    isSecureContext: () => typeof window !== "undefined" && window.isSecureContext === true,
    isVisible: () => typeof document !== "undefined" && document.visibilityState === "visible",
    writeText: typeof navigator !== "undefined" && navigator.clipboard?.writeText
      ? (text) => navigator.clipboard.writeText(text)
      : undefined,
    legacyCopy,
    now: () => Date.now(),
  };
}
