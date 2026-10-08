import { useCallback } from "react";
import type { ConnectionState } from "../../store/workers";
import { buildInterfaceComposerMessage } from "./InterfaceChatControllerModel";
import type { ComposerAttachment } from "./InterfaceChatSession";

interface RouteDraftSubmissionInput {
  draft: string;
  composedText: string;
  previousDraft: string;
  previousAttachments: ComposerAttachment[];
}

interface UseInterfaceDraftSubmissionInput {
  draft: string;
  attachments: ComposerAttachment[];
  connectionState: ConnectionState;
  sending: boolean;
  uploading: boolean;
  routeDraftSubmission(input: RouteDraftSubmissionInput): boolean;
  submitTextToInterface(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
  /** Send was pressed while the server is unreachable. */
  reportOfflineSend(): void;
}

export function useInterfaceDraftSubmission({
  draft,
  attachments,
  connectionState,
  sending,
  uploading,
  routeDraftSubmission,
  submitTextToInterface,
  reportOfflineSend,
}: UseInterfaceDraftSubmissionInput) {
  return useCallback(() => {
    const text = buildInterfaceComposerMessage(draft, attachments);
    if (!text || sending || uploading) {
      return;
    }
    if (connectionState !== "connected") {
      reportOfflineSend();
      return;
    }
    const previousDraft = draft;
    const previousAttachments = attachments;
    if (
      routeDraftSubmission({
        draft,
        composedText: text,
        previousDraft,
        previousAttachments,
      })
    ) {
      return;
    }
    submitTextToInterface(text, previousDraft, previousAttachments);
  }, [
    attachments,
    connectionState,
    draft,
    reportOfflineSend,
    routeDraftSubmission,
    sending,
    submitTextToInterface,
    uploading,
  ]);
}
