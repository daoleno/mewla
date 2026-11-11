import { signal } from "nsh";

/**
 * A Session's terminal is on screen. The terminal owns every key there (Ctrl+K
 * is the shell's kill-line, letters are typing), so the desktop's shortcuts
 * are unbound while it shows. Set by the Session page.
 */
export const terminalShowing = signal(false);

interface ComposerHandle {
  focus(): void;
  /** On screen now: the page is shown and its chat is. */
  active(): boolean;
}

const composers = new Set<ComposerHandle>();

/** A chat composer that `/` may focus; returns its removal. */
export function registerComposer(composer: ComposerHandle): () => void {
  composers.add(composer);
  return () => void composers.delete(composer);
}

/** `/`: the message box of the chat on screen, if there is one. */
export function focusComposer(): void {
  for (const composer of composers) {
    if (composer.active()) return composer.focus();
  }
}
