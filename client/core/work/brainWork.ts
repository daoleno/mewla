import type { CalendarTransport } from "../calendar/calendar";
import type { Frame } from "../transport";
import type { BrainCurrentWork, BrainObjective, BrainOpenWork, BrainWorkDetail, BrainWorkUserActionKind } from "./types";

/** The Work parts of `brain_snapshot.brain`. */
export function workOfSnapshot(snapshot: Record<string, unknown> | null | undefined): {
  current: BrainCurrentWork[];
  open: BrainOpenWork[];
  objective: BrainObjective | null;
} {
  const list = <T>(value: unknown): T[] => (Array.isArray(value) ? (value as T[]).filter((v) => v && typeof v === "object" && typeof (v as { work_id?: unknown }).work_id === "string") : []);
  const objective = snapshot?.objective as BrainObjective | undefined;
  return {
    current: list<BrainCurrentWork>(snapshot?.current_work),
    open: list<BrainOpenWork>(snapshot?.open_work).filter((work) => work.status !== "done" && work.status !== "cancelled"),
    objective: objective && typeof objective.title === "string" && objective.title ? { ...objective, back: Math.min(objective.back, objective.total) } : null,
  };
}

/** "Ship the beta · 3 of 5 back". */
export function objectiveLine(objective: BrainObjective | null): string | null {
  return objective ? `${objective.title} · ${objective.back} of ${objective.total} back` : null;
}

export interface WorkActionResult {
  status?: string;
  /** "uncertain": sent, but Brain may not have it. */
  admission?: string;
}

/**
 * Tells Brain what you decided about a Work: reply (an answer, or free text), close,
 * dismiss, stop (closes its Worker too) or snooze. The daemon broadcasts the new
 * snapshot after it. A timeout means it may still have reached Brain.
 */
export async function workAction(
  transport: CalendarTransport,
  workId: string,
  action: BrainWorkUserActionKind,
  options: { text?: string; snoozeSeconds?: number; attachments?: readonly unknown[] } = {},
): Promise<WorkActionResult> {
  try {
    const frame = await transport.request<Frame>("brain_work_action", {
      id: workId,
      action,
      ...(options.text ? { text: options.text } : {}),
      // Files picked into the reply, already on the computer (core/files uploads).
      ...(options.attachments?.length ? { attachments: options.attachments } : {}),
      ...(options.snoozeSeconds ? { snooze_seconds: options.snoozeSeconds } : {}),
    }, { reply: "brain_work_action", errors: ["error", "brain_work_action_failed"], timeoutMs: 30_000 });
    return { status: frame.status as string | undefined, admission: (frame.admission as string | undefined) || undefined };
  } catch (error) {
    const message = error instanceof Error ? error.message : "";
    throw new Error(/timed out/.test(message) ? "Timed out. The action may still have reached Brain." : message || "Brain could not take that action.");
  }
}

/** One Work's page, current or closed (`brain_work_detail`): who worked on it. */
export async function workDetail(transport: CalendarTransport, workId: string): Promise<BrainWorkDetail> {
  const frame = await transport.request<Frame>("brain_work_detail", { id: workId }, { reply: "brain_work_detail", errors: ["error", "brain_work_detail_failed"], timeoutMs: 15_000 });
  const work = frame.work as BrainWorkDetail | undefined;
  if (!work?.work_id) throw new Error("This Work no longer exists.");
  return { ...work, worker_sessions: work.worker_sessions ?? [] };
}

/** Marks a Work's result read, so a closed Work leaves the list. */
export function markWorkRead(transport: CalendarTransport, workId: string): void {
  try {
    transport.send({ type: "brain_work_read", id: workId });
  } catch {
    // Read again next time it is opened.
  }
}
