import type { CodexConversationEvent } from "../../services/codexConversation";
import type { StoredServer } from "../../services/storage";
import { markdownPreview } from "./taskNotificationCardModel";

/** Daemon source for messages between Brains on the user's computers. */
export const COMPUTER_MESSAGE_SOURCE = "computer_message";

const PREVIEW_MAX_CHARS = 180;

export type ComputerMessageTone = "accent" | "danger" | "attention" | "neutral";

/**
 * A task this Brain handed to another of the user's computers, or that
 * computer's answer. A system card, never a user message: the answer reached
 * Brain as relayed text, which the card shows without the relay's wrapping.
 */
export type ComputerMessageTimelineItem = {
  type: "computer-message";
  id: string;
  timestamp?: string;
  direction: "to" | "from";
  computer: string;
  daemonId?: string;
  kindLabel: string;
  statusLabel?: string;
  tone: ComputerMessageTone;
  /** What the user said, relayed with the task. */
  userSaid?: string;
  body: string;
  preview: string;
};

export function computerMessageTimelineItem(
  event: CodexConversationEvent,
): ComputerMessageTimelineItem | null {
  const message = event.computer;
  if (event.kind !== "status" || event.source !== COMPUTER_MESSAGE_SOURCE || !message) {
    return null;
  }
  const body = (event.body || "").trim();
  const delivery = message.direction === "to" ? DELIVERY[message.state ?? ""] : undefined;
  return {
    type: "computer-message",
    id: event.id || `computer-message:${message.message_id}`,
    timestamp: event.timestamp,
    direction: message.direction,
    computer: message.computer,
    daemonId: message.daemon_id || undefined,
    kindLabel: kindLabel(message.direction, message.kind, message.computer),
    statusLabel: delivery?.label,
    tone: delivery?.tone ?? "neutral",
    userSaid: message.user_said?.trim() || undefined,
    body,
    preview: markdownPreview(body, PREVIEW_MAX_CHARS),
  };
}

function kindLabel(direction: "to" | "from", kind: string, computer: string): string {
  if (direction === "to") {
    switch (kind) {
      case "question": return `Asked ${computer}`;
      case "reply": case "answer": return `Answered ${computer}`;
      case "cancel": return `Cancelled on ${computer}`;
      default: return `Handed to ${computer}`;
    }
  }
  switch (kind) {
    case "request": return `Task from ${computer}`;
    case "question": return `Question from ${computer}`;
    case "answer": return `Answer from ${computer}`;
    case "cancel": return `${computer} cancelled`;
    default: return `Reply from ${computer}`;
  }
}

const DELIVERY: Record<string, { label: string; tone: ComputerMessageTone }> = {
  queued: { label: "Waiting", tone: "neutral" },
  sent: { label: "Sending", tone: "neutral" },
  delivered: { label: "Delivered", tone: "accent" },
  uncertain: { label: "May not have arrived", tone: "attention" },
  failed: { label: "Not delivered", tone: "danger" },
};

/**
 * The stored server for the other computer, so the card can open it there.
 * The daemon may know only the start of that computer's id.
 */
export function serverForComputer(
  servers: readonly StoredServer[],
  daemonId: string | undefined,
  currentServerId: string | null,
): StoredServer | null {
  const id = daemonId?.trim().toLowerCase();
  if (!id || id.length < 16) return null;
  const matches = servers.filter(
    (server) => server.id !== currentServerId && server.daemonId?.toLowerCase().startsWith(id),
  );
  return matches.length === 1 ? matches[0] : null;
}
