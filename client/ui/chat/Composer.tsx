import { effect, keyboardFrame, signal, Text, untracked, View } from "nsh";
import { Button, Input } from "nsh/base";
import type { Draft } from "../../core/conversation/draft";
import { belowSections } from "../shell/Shell";

/**
 * A chat's composer text, kept in the chat's `Draft`: the composer is
 * brought to the draft once per chat (typing owns it afterwards), and what was
 * typed before the chat was known becomes its draft.
 */
export function createComposerText(draft: () => Draft | null) {
  const text = signal("");
  let shownFor = "";
  effect(() => {
    const current = draft();
    const key = current?.cacheKey ?? "";
    if (shownFor === key) return;
    const typedBefore = shownFor === "" ? untracked(text) : "";
    shownFor = key;
    if (!current) return text.set("");
    if (typedBefore) return current.save(typedBefore);
    text.set("");
    void current.load().then((value) => untracked(draft) === current && !untracked(text) && text.set(value));
  });
  return {
    text,
    set(value: string) {
      text.set(value);
      untracked(draft)?.save(value);
    },
    /** Sent: the composer and its draft are empty. */
    clear() {
      text.set("");
      untracked(draft)?.save("");
    },
  };
}
export type ComposerText = ReturnType<typeof createComposerText>;

/**
 * The text field and Send, above the keyboard. While a turn runs and nothing
 * is typed, Send is Stop ("Stopping…" until the turn ends). `children` go
 * above the field (the offline line, a question's card, a notice).
 */
export function Composer(props: {
  text: ComposerText;
  placeholder: string | (() => string);
  /** Focus the field when it appears (a just-created Session). */
  autoFocus?: boolean;
  canSend: () => boolean;
  /** Returns false to keep the text (an edit that sends once its rewind is done). */
  onSend: (text: string) => void | false;
  running: () => boolean;
  stopping: () => boolean;
  canStop: () => boolean;
  onStop: () => void;
  /** Whether the page is on screen: only then does it lift for the keyboard. */
  active?: () => boolean;
  /** Left of the field (a Session's "+"). */
  leading?: unknown;
  children?: unknown;
}) {
  const value = () => props.text.text();
  const send = () => {
    const text = value();
    if (!props.canSend() || !text.trim()) return;
    if (props.onSend(text) !== false) props.text.clear();
  };
  return (
    <View class="border-t border-border bg-background px-3 py-2"
      style={{ marginBottom: props.active && !props.active() ? 0 : Math.max(0, keyboardFrame().height - belowSections()) }}>
      {props.children as never}
      <View class="flex-row items-end gap-2">
        {props.leading as never}
        <Input testID="composer" multiline value={value()} autoFocus={props.autoFocus}
          placeholder={typeof props.placeholder === "function" ? props.placeholder() : props.placeholder}
          class="max-h-40 flex-1 rounded-2xl border border-border bg-card px-3.5 py-2.5 text-base"
          onChange={({ value }) => props.text.set(value)} />
        {props.running() && !value().trim() ? (
          <Button testID="stop" onPress={props.onStop} disabled={props.stopping() || !props.canStop()} class="rounded-full border border-border px-4 py-2.5 active:bg-accent">
            <Text class="text-base font-semibold">{props.stopping() ? "Stopping…" : "Stop"}</Text>
          </Button>
        ) : (
          <Button testID="send" onPress={send} disabled={!props.canSend() || !value().trim()} class="rounded-full bg-primary px-4 py-2.5 active:opacity-80">
            <Text class="text-base font-semibold text-primary-foreground">Send</Text>
          </Button>
        )}
      </View>
    </View>
  );
}
