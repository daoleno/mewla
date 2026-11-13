import type { Frame } from "../transport";

/**
 * A new Session's first message (WIRE §11): create_session carries `text`, a
 * daemon that holds it answers `first_message: "pending"`, and later tells
 * every connection how it went (`session_first_message`).
 */
export type FirstMessageOutcome = "sent" | "not_sent" | "unknown";

export type FirstMessageNews = { workerId: string; outcome: FirstMessageOutcome; text: string };

/** A `session_first_message` frame, or null for one that is not well formed. */
export function firstMessageNews(frame: Frame): FirstMessageNews | null {
  const outcome = frame.outcome;
  if (typeof frame.worker_id !== "string" || !frame.worker_id) return null;
  if (outcome !== "sent" && outcome !== "not_sent" && outcome !== "unknown") return null;
  return { workerId: frame.worker_id, outcome, text: typeof frame.text === "string" ? frame.text : "" };
}
