import { useCallback } from "react";
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
  sending: boolean;
  uploading: boolean;
  routeDraftSubmission(input: RouteDraftSubmissionInput): boolean;
  submitTextToInterface(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
}

export function useInterfaceDraftSubmission({
  draft,
  attachments,
  sending,
  uploading,
  routeDraftSubmission,
  submitTextToInterface,
}: UseInterfaceDraftSubmissionInput) {
  return useCallback(() => {
    const text = buildInterfaceComposerMessage(draft, attachments);
    if (!text || sending || uploading) {
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
    draft,
    routeDraftSubmission,
    sending,
    submitTextToInterface,
    uploading,
  ]);
}
