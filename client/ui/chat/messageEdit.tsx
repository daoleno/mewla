import { Clipboard, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import type { MessageTimelineItem } from "../../core/timeline/types";
import { isEditablePrompt, messageCopyText, messageEditMode, type MessageEdit } from "../../core/timeline/messageActionsModel";
import { composerEdits, type ComposerText } from "./Composer";
import { showAlert } from "../shell/alert";

/** The text the Select Text sheet shows; "" while it is closed. */
export const selectingText = signal("");

const INPUT_NOT_CLEAN = "Rewound, but the agent's input box still has text. Clear it in the Terminal, then send.";

/**
 * Edit puts one of the person's messages in the composer. Sending it either
 * rewinds the agent to before that message and then sends the new text (a
 * Session whose CLI can rewind), or sends it as a new message (Brain, and
 * every other Session). A rewind that fails sends nothing and leaves an honest
 * copy; one that leaves the agent's input box dirty is done and not repeated.
 */
export function createMessageEdit(options: {
  text: ComposerText;
  brain: boolean;
  messageRewind: () => boolean;
  rewind?: (eventId: string) => Promise<{ composerClean: boolean }>;
  send: (text: string) => void;
}) {
  const edit = signal<MessageEdit | null>(null);
  const error = signal("");
  const busy = signal(false);
  let saved = "";
  const open = (item: MessageTimelineItem) => {
    if (busy()) return;
    if (!untracked(edit)) saved = untracked(options.text.text);
    edit.set({ id: item.id, original: item.body, mode: messageEditMode({ messageRewind: options.messageRewind(), brain: options.brain }) });
    error.set("");
    options.text.set(item.body);
  };
  const cancel = () => {
    if (busy()) return;
    edit.set(null);
    error.set("");
    options.text.set(saved);
  };
  /** The composer's Send while editing; it clears the composer itself once the text went out. */
  const send = async (value: string) => {
    const current = untracked(edit);
    if (!current || busy() || !value.trim()) return;
    if (current.mode === "rewind" && !current.rewound && options.rewind) {
      busy.set(true);
      error.set("");
      try {
        const { composerClean } = await options.rewind(current.id);
        if (!composerClean) {
          edit.set({ ...current, rewound: true });
          error.set(INPUT_NOT_CLEAN);
          return;
        }
      } catch (reason) {
        edit.set({ ...current, mode: "copy" });
        error.set(reason instanceof Error && reason.message ? reason.message : "Could not rewind.");
        return;
      } finally {
        busy.set(false);
      }
    }
    edit.set(null);
    error.set("");
    options.send(value);
    options.text.set(saved);
  };
  composerEdits.set(options.text, { open: () => edit() !== null, cancel });
  return { edit, error, busy, open, cancel, send };
}
export type MessageEditor = ReturnType<typeof createMessageEdit>;

/** "Editing message" above the composer, with what it was and Cancel. */
export function EditChip(props: { editor: MessageEditor }) {
  const e = props.editor;
  return (
    <View testID="edit-chip" class="mb-1.5 gap-1 rounded-xl bg-secondary px-3 py-2">
      <View class="flex-row items-center gap-2">
        <Text class="flex-1 text-xs font-semibold uppercase text-muted-foreground">{e.busy() ? "Rewinding…" : e.edit()?.mode === "rewind" && !e.edit()?.rewound ? "Editing · rewinds the agent" : "Editing message"}</Text>
        <Button testID="edit-cancel" onPress={e.cancel} disabled={e.busy()}><Text class="text-sm text-primary">Cancel</Text></Button>
      </View>
      <Text class="text-sm text-muted-foreground line-clamp-2">{e.edit()?.original ?? ""}</Text>
      {e.error() ? <Text testID="edit-error" class="text-sm text-destructive">{e.error()}</Text> : null}
    </View>
  );
}

/** The menu of a message: Copy, and Edit for a sent prompt of a connected, idle chat. */
export async function showMessageMenu(item: MessageTimelineItem, env: { editor?: MessageEditor; connected: boolean; running: boolean }): Promise<void> {
  const subject = { id: item.id, role: item.role, body: item.body, hasAttachments: item.attachments.length > 0, pending: Boolean(item.pending), streaming: Boolean(item.streaming) };
  if (subject.streaming || !subject.body.trim()) return;
  const editable = env.editor && isEditablePrompt(subject, { messageRewind: false, brain: false, running: env.running, connected: env.connected, canSelectText: false });
  const { action } = await showAlert({
    presentation: "actionSheet",
    actions: [
      { id: "copy", label: "Copy" },
      { id: "select", label: "Select Text" },
      ...(editable ? [{ id: "edit", label: "Edit" }] : []),
      { id: "cancel", label: "Cancel", role: "cancel" as const },
    ],
  });
  if (action === "copy") await Clipboard.writeText(messageCopyText(subject)).catch(() => {});
  else if (action === "select") selectingText.set(subject.body);
  else if (action === "edit") env.editor?.open(item);
}
