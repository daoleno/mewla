import { composerKeyIntent } from "../navigation/desktopShortcuts";

export const COMPOSER_SUBMIT_BEHAVIOR = "newline" as const;

export function composerReturnKeyType(platform: string) {
  return platform === "android" ? ("none" as const) : ("default" as const);
}

/**
 * No touchscreen as the primary pointer: there Enter sends (desktop web).
 * Phones and tablets report a coarse pointer and keep Enter as a new line.
 */
export function hasDesktopPointer(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    !window.matchMedia("(pointer: coarse)").matches
  );
}

/**
 * Enter and Esc for a small web text box that is not the chat composer (a
 * Work reply): Enter sends, Shift+Enter is a new line, nothing acts during
 * IME composition, Esc leaves the box. The caller wires it to onKeyPress.
 */
export function webReplyKeyPress(
  event: { nativeEvent: unknown; preventDefault(): void },
  { canSend, send, leave }: { canSend: boolean; send(): void; leave(): void },
): void {
  const intent = composerKeyIntent(event.nativeEvent as KeyboardEvent, {
    desktop: hasDesktopPointer(),
    draftEmpty: false,
  });
  if (intent === "send") {
    event.preventDefault();
    if (canSend) send();
  } else if (intent === "blur") {
    leave();
  }
}
