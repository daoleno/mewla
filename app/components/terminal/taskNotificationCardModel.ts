import type { CodexConversationEvent } from "../../services/codexConversation";

/** Daemon source for provider background-task notifications. */
export const TASK_NOTIFICATION_SOURCE = "task_notification";

const PREVIEW_MAX_CHARS = 180;

export type TaskNotificationStatus = "done" | "failed" | "killed" | "event";
export type TaskNotificationTone = "accent" | "danger" | "attention" | "neutral";
export type TaskNotificationIcon =
  | "checkmark-circle"
  | "alert-circle"
  | "stop-circle"
  | "notifications";

export type TaskNotificationDetail = { label: string; value: string };

/**
 * A provider background task (subagent, background command, monitor) that
 * reported back into the conversation. It is a system card, never a user
 * message, so pending-message reconciliation and user styling never see it.
 */
export type TaskNotificationTimelineItem = {
  type: "task-notification";
  id: string;
  timestamp?: string;
  kindLabel: string;
  title: string;
  detail?: string;
  status: TaskNotificationStatus;
  statusLabel: string;
  tone: TaskNotificationTone;
  icon: TaskNotificationIcon;
  facts: string[];
  /** Full result Markdown; shown only when expanded. */
  body: string;
  /** Plain one-paragraph excerpt of the body for the collapsed card. */
  preview: string;
  details: TaskNotificationDetail[];
};

export function taskNotificationTimelineItem(
  event: CodexConversationEvent,
): TaskNotificationTimelineItem | null {
  if (event.kind !== "status" || event.source !== TASK_NOTIFICATION_SOURCE) {
    return null;
  }
  const status = normalizeStatus(event.status);
  const summary = parseSummary((event.title || "").trim());
  const body = (event.body || "").trim();
  const facts = [
    event.task_duration_ms != null
      ? formatTaskDuration(event.task_duration_ms)
      : "",
    event.task_tool_uses != null
      ? `${event.task_tool_uses} tool ${event.task_tool_uses === 1 ? "use" : "uses"}`
      : "",
    event.task_tokens != null ? `${formatTaskTokens(event.task_tokens)} tokens` : "",
  ].filter(Boolean);
  const details: TaskNotificationDetail[] = [];
  if (event.task_id?.trim()) {
    details.push({ label: "Task", value: event.task_id.trim() });
  }
  if (event.call_id?.trim()) {
    details.push({ label: "Tool call", value: event.call_id.trim() });
  }
  return {
    type: "task-notification",
    id: event.id || `task-notification:${event.seq}`,
    timestamp: event.timestamp,
    kindLabel: summary.kind,
    title: summary.title,
    detail: summary.detail,
    status,
    ...STATUS_PRESENTATION[status],
    facts,
    body,
    preview: markdownPreview(body, PREVIEW_MAX_CHARS),
    details,
  };
}

const STATUS_PRESENTATION: Record<
  TaskNotificationStatus,
  { statusLabel: string; tone: TaskNotificationTone; icon: TaskNotificationIcon }
> = {
  done: { statusLabel: "Done", tone: "accent", icon: "checkmark-circle" },
  failed: { statusLabel: "Failed", tone: "danger", icon: "alert-circle" },
  killed: { statusLabel: "Stopped", tone: "attention", icon: "stop-circle" },
  event: { statusLabel: "Update", tone: "neutral", icon: "notifications" },
};

function normalizeStatus(value: string | undefined): TaskNotificationStatus {
  return value === "done" || value === "failed" || value === "killed"
    ? value
    : "event";
}

const SUMMARY_KINDS: ReadonlyArray<[RegExp, string]> = [
  [/^Agent$/i, "Agent"],
  [/^Monitor event:?$/i, "Monitor"],
  [/^Monitor$/i, "Monitor"],
  [/^Background command$/i, "Command"],
];
const GENERIC_SUMMARY_TAILS = new Set(["", "finished", "completed", "done"]);

/**
 * Claude summaries read `Agent "Name" finished`. Lift the quoted name to the
 * title and keep any non-generic tail (a failure reason) as the detail line.
 */
function parseSummary(summary: string): {
  kind: string;
  title: string;
  detail?: string;
} {
  const match = /^([A-Za-z][A-Za-z ]*?:?)\s+"([^"]+)"\s*(.*)$/s.exec(summary);
  const kind = match
    ? SUMMARY_KINDS.find(([pattern]) => pattern.test(match[1].trim()))?.[1]
    : undefined;
  if (!match || !kind) {
    return { kind: "Background task", title: summary || "Background task" };
  }
  const tail = match[3].trim();
  return {
    kind,
    title: match[2].trim(),
    detail: GENERIC_SUMMARY_TAILS.has(tail.toLowerCase())
      ? undefined
      : capitalize(tail),
  };
}

function capitalize(value: string) {
  return value ? value[0].toUpperCase() + value.slice(1) : value;
}

export function formatTaskDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.round(ms / 1000));
  if (totalSeconds < 60) {
    return `${totalSeconds}s`;
  }
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  }
  return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
}

export function formatTaskTokens(tokens: number): string {
  if (tokens < 1000) {
    return String(tokens);
  }
  if (tokens < 1_000_000) {
    const value = tokens / 1000;
    return `${value < 100 ? value.toFixed(1).replace(/\.0$/, "") : Math.round(value)}k`;
  }
  return `${(tokens / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
}

/** First readable paragraph of Markdown as plain text, capped for a preview. */
export function markdownPreview(markdown: string, maxChars: number): string {
  const paragraph =
    markdown
      .replace(/```[\s\S]*?(```|$)/g, "\n\n")
      .split(/\n\s*\n/)
      .map((block) =>
        block
          .split("\n")
          .map((line) => line.replace(/^\s{0,3}(#{1,6}\s+|[-*+]\s+|>\s?|\d+\.\s+)/, ""))
          .join(" "),
      )
      .map((block) =>
        block
          .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
          .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
          .replace(/(\*\*|__|`)/g, "")
          .replace(/\s+/g, " ")
          .trim(),
      )
      .find(Boolean) ?? "";
  if (paragraph.length <= maxChars) {
    return paragraph;
  }
  return `${paragraph.slice(0, maxChars - 1).trimEnd()}…`;
}
