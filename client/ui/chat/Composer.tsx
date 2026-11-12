import { cleanup, effect, keyboardFrame, keymap, platform, safeArea, signal, Text, untracked, View, type InputRef } from "nsh";
import { Button, Input } from "nsh/base";
import { Icon } from "../icons/Icon";
import type { Draft } from "../../core/conversation/draft";
import { appendToDraft } from "../../core/conversation/draftText";
import { composerKeyIntent } from "../../core/web/shortcuts";
import { registerComposer } from "../shell/desktop/keys";
import { belowSections } from "../shell/Shell";

/** The composers that show a chat's draft now, by its cache key: text written from elsewhere goes into them. */
const writers = new Map<string, Set<(text: string) => void>>();

/**
 * Adds `text` to a chat's draft from outside its composer (Work › Ask Brain about this, SPEC §3.4, §3.8):
 * a composer holding that draft shows it at once, below what is typed there; with none, the saved draft
 * takes it, and the composer shows it when it opens.
 */
export function writeIntoDraft(draft: Draft | null, text: string): void {
  if (!draft) return;
  const open = writers.get(draft.cacheKey);
  if (open?.size) return open.forEach((write) => write(text));
  void draft.load().then((saved) => draft.save(appendToDraft(saved, text)));
}

/**
 * A chat's composer text, kept in the chat's `Draft`: the composer is
 * brought to the draft once per chat (typing owns it afterwards), and what was
 * typed before the chat was known becomes its draft. Text written into the
 * draft from elsewhere (`writeIntoDraft`) joins what is typed.
 */
export function createComposerText(draft: () => Draft | null) {
  const text = signal("");
  let shownFor = "";
  // The saved draft read in: text written from elsewhere waits for it, so it is added to the draft, not lost under it.
  let loaded: Promise<unknown> = Promise.resolve();
  effect(() => {
    const current = draft();
    const key = current?.cacheKey ?? "";
    if (shownFor === key) return;
    const typedBefore = shownFor === "" ? untracked(text) : "";
    shownFor = key;
    if (!current) return text.set("");
    if (typedBefore) return current.save(typedBefore);
    text.set("");
    loaded = current.load().then((value) => untracked(draft) === current && !untracked(text) && text.set(value));
  });
  // A page that goes (home is made again when a link replaces the stack) keeps what was just typed: the
  // next composer of the chat reads it back.
  cleanup(() => untracked(draft)?.flush());
  effect(() => {
    const current = draft();
    if (!current) return;
    const write = (added: string) => void loaded.then(() => {
      if (untracked(draft) !== current) return;
      text.set(appendToDraft(untracked(text), added));
      current.save(untracked(text));
    });
    const set = writers.get(current.cacheKey) ?? new Set();
    set.add(write);
    writers.set(current.cacheKey, set);
    cleanup(() => {
      set.delete(write);
      if (!set.size) writers.delete(current.cacheKey);
    });
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

/** The message edit working on a composer's text (`createMessageEdit` registers it): Escape leaves it. */
export const composerEdits = new WeakMap<ComposerText, { open: () => boolean; cancel: () => void }>();

/**
 * The text field and Send, above the keyboard. While a turn runs and nothing
 * is typed, Send is Stop ("Stopping…" until the turn ends). `children` go
 * above the field (the offline line, a question's card, a notice).
 *
 * With a keyboard and a pointer (`platform.desktop`: the desktops, a desktop
 * browser) Return sends and Shift+Return is a new line; Ctrl/⌘+Return sends
 * too; a Return that commits an input method's text never sends (NSH's
 * `submitBehavior`); Escape leaves the field; `/` on the page focuses the
 * field. ↑ in the empty field edits the last message (`onRecall`) and Escape
 * leaves that edit, on every platform with a hardware keyboard.
 *
 * A picture or file pasted into the field becomes a chip (`onFiles`).
 *
 * Text put in from outside (a picked `@agent` or skill, a recalled message) leaves
 * the caret at its end. `disabled` (soft-final/SPEC.md §11): the field and Send at
 * 40 %, nothing sends, and the placeholder says why (`disabledReason`).
 */
export function Composer(props: {
  text: ComposerText;
  placeholder: string | (() => string);
  /** Focus the field when it appears (a just-created Session). */
  autoFocus?: boolean;
  canSend: () => boolean;
  /** Chips ready to go: with them Send sends even with no text (SPEC §1.4). */
  attached?: () => boolean;
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
  /** A slash-command list is open over the field: Return waits for a pick instead of sending. */
  pickerOpen?: () => boolean;
  /** ↑ in the empty field: put the last message in it to edit. */
  onRecall?: () => void;
  /** Nothing can be sent now (Brain's chat is not available, did not load, can't start). */
  disabled?: () => boolean;
  /** The placeholder while disabled: why. */
  disabledReason?: () => string;
  /**
   * Pictures and files pasted into the field (a screenshot, an image copied from a page; the web, and
   * a phone keyboard's image): they become chips as picked ones do (`AttachmentUploads.add`). Pasted
   * text goes in as typed.
   */
  onFiles?: (files: File[]) => void;
  children?: unknown;
}) {
  const value = () => props.text.text();
  const disabled = () => Boolean(props.disabled?.());
  const send = () => {
    const text = value();
    if (disabled() || !props.canSend() || (!text.trim() && !props.attached?.())) return;
    if (props.onSend(text) !== false) props.text.clear();
  };
  const desktop = platform.desktop;
  const context = () => ({ desktop, draftEmpty: !untracked(value), pickerOpen: props.pickerOpen ? untracked(props.pickerOpen) : false });
  const returned = () => {
    if (composerKeyIntent({ key: "Enter" }, context()) === "send") send();
  };
  let input: InputRef | undefined;
  const focused = signal(false);
  // What the field last reported typing: any other value was put in from outside, and the caret goes to its end
  // (it stayed where it was, so a key after a picked "@agent " landed inside it).
  let typed = "";
  effect(() => {
    const next = value();
    if (next === typed) return;
    typed = next;
    setTimeout(() => input?.setSelection(next.length), 0);
  });
  cleanup(registerComposer({ focus: () => input?.focus(), active: () => (props.active ? props.active() : true) }));
  // ↑ is the caret's key: bound only while the field has the focus and is empty, so it moves the caret
  // otherwise. Everywhere a key arrives: the desktops, the web, a phone's hardware keyboard.
  effect(() => {
    if (!props.onRecall || !focused() || value()) return;
    keymap.bind("up", () => {
      if (composerKeyIntent({ key: "ArrowUp" }, context()) === "recall") props.onRecall?.();
    }, { when: "always" });
  });
  // A page pushed over this one (the git page from a Session) or another tab: the field lets go, and
  // its keyboard goes down instead of covering a page that has no field (R4#4).
  effect(() => {
    if (!props.active || props.active()) return;
    untracked(() => {
      if (focused()) input?.blur();
    });
  });
  // Escape leaves an open message edit (its chip's Cancel), wherever a hardware keyboard is, while the
  // page is shown (SPEC §1.4); with no edit, it leaves the field on a desktop (the desktop's `/` went
  // into it), so the page's keys work again. A list open over the field keeps it.
  const edit = composerEdits.get(props.text);
  effect(() => {
    const editing = Boolean(edit?.open());
    if (editing ? !(props.active ? props.active() : true) : !(desktop && focused())) return;
    keymap.bind("escape", () => {
      if (props.pickerOpen && untracked(props.pickerOpen)) return;
      if (edit && untracked(edit.open)) edit.cancel();
      else input?.blur();
    }, { when: "always" });
  });
  const empty = () => !value().trim() && !props.attached?.();
  // While a turn runs, an empty composer's button is Stop (SPEC §1.4); with text or chips it sends.
  const stops = () => props.running() && !value().trim() && !props.attached?.();
  const placeholder = () => (disabled() && props.disabledReason ? props.disabledReason() : typeof props.placeholder === "function" ? props.placeholder() : props.placeholder);
  return (
    // The dock (soft-final SPEC §11 Composer): on the canvas, 8 above and 12 at the sides, no rule above it.
    <View testID="composer-dock" class="bg-background px-3 pt-2"
      // Above the keyboard when it is up; else clear of the gesture bar (Android draws edge to edge), which lies under the stack.
      style={{
        marginBottom: props.active && !props.active() ? 0 : Math.max(0, keyboardFrame().height - belowSections()),
        paddingBottom: 10 + (keyboardFrame().height > 0 ? 0 : Math.max(0, safeArea().bottom - belowSections())),
      }}>
      {props.children as never}
      {/* One surface pill, 52 high and growing to 128: attach inside at the left, Send inside at the right. */}
      <View testID="composer-field" class={`min-h-[52px] flex-row items-end rounded-[26px] bg-card pl-1 pr-1 ${disabled() ? "opacity-40" : ""}`}>
        {props.leading as never}
        <Input testID="composer" multiline value={value()} autoFocus={props.autoFocus} ref={(ref) => (input = ref)}
          placeholder={placeholder()}
          disabled={disabled()}
          class="max-h-[128px] min-h-[52px] min-w-0 flex-1 bg-transparent px-1 py-3.5 text-body text-foreground placeholder:text-faint"
          submitBehavior={desktop ? "submit" : undefined} onSubmit={returned}
          onFocus={() => focused.set(true)} onBlur={() => focused.set(false)}
          onPaste={props.onFiles ? ({ files }) => void (files.length && !disabled() && props.onFiles?.(files)) : undefined}
          onChange={({ value }) => {
            typed = value;
            props.text.set(value);
          }} />
        {stops() ? (
          <SendButton testID="stop" label={props.stopping() ? "Stopping…" : "Stop"} onPress={props.onStop} disabled={props.stopping() || !props.canStop()} ready={false}>
            <View class="size-3 rounded-[3.5px] bg-foreground" />
          </SendButton>
        ) : (
          <SendButton testID="send" label="Send" onPress={send} disabled={disabled() || !props.canSend() || empty()} ready={!disabled() && props.canSend() && !empty()}>
            <Icon name="arrow-up" size={20} color={!disabled() && props.canSend() && !empty() ? "card" : "faint"} />
          </SendButton>
        )}
      </View>
    </View>
  );
}

/** Send (or Stop): a 36 pt circle in a 48 pt hit, canvas when there is nothing to send, ink when ready. */
function SendButton(props: { testID: string; label: string; ready: boolean; disabled: boolean; onPress: () => void; children: unknown }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.label} onPress={props.onPress} disabled={props.disabled}
      class="mb-0.5 size-12 items-center justify-center active:opacity-70">
      <View class={`size-9 items-center justify-center rounded-full ${props.ready ? "bg-primary" : "bg-background"}`}>{props.children as never}</View>
    </Button>
  );
}
