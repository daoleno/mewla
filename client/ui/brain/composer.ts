import { cleanup, computed, signal, untracked } from "nsh";
import { brainExecutorChoices } from "../../core/brainMenu";
import { writeIntoDraft, type ComposerText } from "../chat/Composer";
import type { ComposerAttach } from "../chat/Attach";
import { createAttachmentUploads } from "../more/files/Pages";
import { registerShareTarget } from "../more/files/shareIn";
import { markNewestUploads } from "../more/files/newestUploads";
import { brainDraft } from "../session";
import { MAX_COMPOSER_ATTACHMENTS } from "../../core/files/uploads";
import { mentionPanelOpen } from "./MentionPanel";
import { currentBrain, type BrainSource } from "./source";

/**
 * What Brain's composer does besides typing (soft-final/SPEC.md §1.4): attach
 * (the files go with the next message: "+" and its sheet, `attach`) and `@` (the
 * agents, `MentionPanel`). What is typed is sent as typed, a leading `/` too. Make
 * one inside the component that holds the composer; it reads the current
 * computer's Brain (`source`, `currentBrain` by default).
 *
 * The host renders: the Composer with `send` as `onSend`, `pickerOpen`, `canSend`
 * and `attach`; AttachmentTray (`uploads`); MentionPanel (`executors`).
 */
export function createBrainComposer(options: {
  text: ComposerText;
  /** A message to show (an attach that failed). */
  onNotice?: (text: string) => void;
  /** A message went out from the composer: the thread brings it to the top, with the reply under it. */
  onSent?: () => void;
  source?: BrainSource;
}) {
  const brain = options.source ?? currentBrain;
  const source = { ...brain, text: options.text, onNotice: options.onNotice };
  const executors = computed(() => brainExecutorChoices(source.snapshot()?.executors ?? [], source.snapshot()?.executor_states).choices);

  const uploads = createAttachmentUploads();
  cleanup(() => uploads.dispose());
  markNewestUploads(uploads);
  // What other apps share to Mewla lands here: the files as chips, the text in the draft (more/files/shareIn.ts).
  cleanup(registerShareTarget({
    room: () => MAX_COMPOSER_ATTACHMENTS - uploads.items().length,
    addFiles: (assets) => uploads.addShared(assets),
    addText: (added) => writeIntoDraft(untracked(brainDraft), added),
    notice: (text) => options.onNotice?.(text),
  }));
  const uploading = () => uploads.items().some((item) => item.status === "uploading" || item.status === "queued");

  return {
    executors: () => executors(),
    uploads,
    uploading,
    /** Send waits while a file uploads, and needs Brain's chat. */
    canSend: () => Boolean(source.chat()) && !uploading(),
    /** The `@` agents' list is open over the field: Return waits for a pick. */
    pickerOpen: () => mentionPanelOpen(source.text.text(), executors()),
    /** The Composer's `attach`: "+" and its sheet, while the computer is connected. */
    attach: {
      uploads,
      enabled: () => source.connected(),
      onError: (message) => source.onNotice?.(message),
    } satisfies ComposerAttach,
    /** The Composer's `onSend`: the text as typed, with the files. */
    send: (value: string): void => {
      source.chat()?.send(value, uploads.ready());
      uploads.clear();
      options.onSent?.();
    },
  };
}
