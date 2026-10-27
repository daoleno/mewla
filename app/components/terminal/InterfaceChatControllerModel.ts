import { DateStyle, formatDate } from "../../services/dateFormat";
import type { ConnectionState } from "../../store/workers";
import type {
  CodexConversation,
  ProviderActivity,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { ComposerAttachment } from "./InterfaceChatSession";

export function buildInterfaceStatusMeta(input: {
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  runningActivity?: ProviderActivity;
  sending: boolean;
}) {
  const {
    connectionState,
    connectionIssue,
    conversation,
    runningActivity,
    sending,
  } = input;
  if (connectionIssue) {
    return connectionIssue.title;
  }
  if (connectionState === "connecting") {
    return "Reconnecting";
  }
  if (connectionState !== "connected") {
    return "Offline";
  }
  if (runningActivity) {
    return "Working";
  }
  if (sending) {
    return "Sending";
  }
  if (conversation?.updated_at) {
    return `Updated ${formatTime(conversation.updated_at)}`;
  }
  return "Live";
}

/**
 * What the composer sends: the person's text and the files they attached, as
 * separate fields. The daemon is the only place that turns them into provider
 * input. Null while an upload is still running or when there is nothing to send.
 */
export function buildInterfaceComposerMessage(
  draft: string,
  attachments: ComposerAttachment[],
): { text: string; attachments: ComposerAttachment[] } | null {
  if (attachments.some((item) => item.uploadStatus && item.uploadStatus !== "ready")) return null;
  const text = draft.trim();
  if (!text && attachments.length === 0) return null;
  return { text, attachments };
}

export function conversationUnavailableReason(reason?: string) {
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

export function isConversationSyncingReason(reason?: string) {
  return (
    reason === "session_not_ready" ||
    reason === "transcript_not_found" ||
    reason === "missing_cwd" ||
    reason === "not_visible"
  );
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return formatDate(date, DateStyle.time);
}
