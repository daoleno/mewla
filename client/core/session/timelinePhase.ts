/** From the Expo app's components/terminal/interfaceTimelinePhase.ts, with the unavailable reasons of InterfaceChatControllerModel.ts. */
/**
 * What an empty timeline is showing. Loading and syncing mean history may
 * still arrive, so they never read as an empty conversation: only a loaded,
 * available conversation with no rows is "empty".
 */
export type InterfaceTimelinePhase =
  | "content"
  | "hidden"
  | "loading"
  | "error"
  | "unavailable"
  | "empty";

export function interfaceTimelinePhase({
  itemCount,
  loading,
  error,
  suppressed,
  unavailable,
  syncing,
}: {
  itemCount: number;
  loading: boolean;
  error?: string | null;
  suppressed: boolean;
  unavailable: boolean | null;
  syncing: boolean;
}): InterfaceTimelinePhase {
  if (itemCount > 0) return "content";
  if (suppressed) return "hidden";
  if (loading) return "loading";
  if (error) return "error";
  if (syncing) return "loading";
  if (unavailable) return "unavailable";
  return "empty";
}

/** Why a Session's chat is not available, in the person's words. */
export function conversationUnavailableReason(reason?: string): string {
  switch (reason) {
    case "not_codex":
      return "Chat is only available for supported Worker sessions.";
    case "not_visible":
      return "This chat is not available from the current view yet.";
    case "missing_cwd":
      return "This chat is still getting its workspace ready.";
    case "transcript_not_found":
      return "Messages are still syncing for this session.";
    case "transcript_malformed":
      return "Chat could not read this session transcript. Open the terminal view.";
    case "worker_not_found":
      return "This Worker session is no longer available.";
    case "session_not_ready":
      return "This chat is getting ready.";
    default:
      return "Open the terminal view for this session.";
  }
}

/** Reasons that pass on their own: the chat shows as loading, not unavailable. */
export function isConversationSyncingReason(reason?: string): boolean {
  return reason === "session_not_ready" || reason === "transcript_not_found" || reason === "missing_cwd" || reason === "not_visible";
}
