import { computed, platform, signal } from "nsh";

/** A Session's terminal is on screen. Set by the Session page. */
export const terminalShowing = signal(false);

/**
 * The terminal owns every key (Ctrl+K is the shell's kill-line, letters are typing), so the
 * desktop's shortcuts step aside for it. In a browser the terminal is its own frame: while it has
 * the focus its keys never reach the page, and with the focus elsewhere (the sidebar, a header)
 * the shortcuts work, as the Expo web app's did (parity #27). Elsewhere the terminal showing is enough.
 */
export const terminalOwnsKeys = computed(() => terminalShowing() && platform.os !== "web");

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
