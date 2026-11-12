/**
 * Answered requests (daemon WIRE §2): one durable record per answered prompt, approval, question and
 * Work question — what, the answer, who, when. A record seals its card everywhere at once (the
 * `request_answered` broadcast) and stays as its sealed line in the thread, after a restart too.
 */
import { looksLikeCommand, type SealedRequest } from "./requestCards";

export type AnsweredOutcome = "answered" | "approved" | "denied" | "not_answered" | "dismissed";

export interface AnsweredRequest {
  id: string;
  kind: string;
  session_id?: string;
  work_id?: string;
  thread_id?: string;
  title?: string;
  asked: string;
  answer?: string;
  outcome: AnsweredOutcome;
  by: { kind: "device" | "brain" | "operator" | "terminal" | "timeout"; device_id?: string; device_name?: string; basis?: string };
  asked_at?: string;
  answered_at: string;
  anchor?: { call_id?: string; prompt_id?: string; approval_id?: string; rpc_id?: string };
}

const OUTCOMES = new Set<AnsweredOutcome>(["answered", "approved", "denied", "not_answered", "dismissed"]);
const BY = new Set(["device", "brain", "operator", "terminal", "timeout"]);
const str = (value: unknown) => (typeof value === "string" && value.trim() ? value.trim() : undefined);

export function parseAnsweredRequest(raw: unknown): AnsweredRequest | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const by = (r.by ?? {}) as Record<string, unknown>;
  const id = str(r.id), answeredAt = str(r.answered_at), outcome = r.outcome as AnsweredOutcome;
  if (!id || !answeredAt || !OUTCOMES.has(outcome) || !BY.has(by.kind as string)) return null;
  const anchor = (r.anchor && typeof r.anchor === "object" ? r.anchor : {}) as Record<string, unknown>;
  return {
    id, kind: str(r.kind) ?? "", session_id: str(r.session_id), work_id: str(r.work_id), thread_id: str(r.thread_id), title: str(r.title),
    asked: str(r.asked) ?? "", answer: str(r.answer), outcome,
    by: { kind: by.kind as AnsweredRequest["by"]["kind"], device_id: str(by.device_id), device_name: str(by.device_name), basis: str(by.basis) },
    asked_at: str(r.asked_at), answered_at: answeredAt,
    anchor: { call_id: str(anchor.call_id), prompt_id: str(anchor.prompt_id), approval_id: str(anchor.approval_id), rpc_id: str(anchor.rpc_id) },
  };
}

/** The keys of the cards (and tray rows) a record answers: the client's keys for the same request. */
export function answeredKeys(record: AnsweredRequest): string[] {
  const keys: string[] = [];
  const session = record.session_id, a = record.anchor ?? {};
  if (session && a.approval_id) keys.push(`approval:${session}:${a.approval_id}`);
  if (session && a.prompt_id) keys.push(`prompt:${session}:${a.prompt_id}`);
  if (a.call_id) keys.push(`brain-choice:${a.call_id}`);
  if (record.kind === "work_question" && record.work_id) keys.push(`work:${record.work_id}`);
  return keys;
}

const VERB: Record<AnsweredOutcome, string> = { approved: "Approved", denied: "Denied", answered: "Answered", not_answered: "Not answered", dismissed: "Dismissed" };

/** Who answered, as the line's source: "Pixel 8", "Brain", "In the terminal". */
export function answeredBy(record: AnsweredRequest): string {
  switch (record.by.kind) {
    case "device": return record.by.device_name || "A device";
    case "brain": return "Brain";
    case "operator": return "From the computer";
    case "terminal": return "In the terminal";
    case "timeout": return "Timed out";
  }
}

/**
 * The record as its sealed line: the outcome, the answer (else what was asked), and who and when.
 * `at` formats the answer's time. Brain's thread shows it when Brain asked or a Work's Worker did.
 */
/**
 * A terminal prompt's record: its `asked` is the screen run together (a Tip line, the tool's summary,
 * the command, "Do you want to proceed?"), never shown as the answer; its title ("Bash command") is.
 */
const SCREEN_PROMPTS = new Set(["permission", "plan", "startup"]);

export function sealedOfAnswered(record: AnsweredRequest, at: (ms: number) => string): SealedRequest {
  const answered = Date.parse(record.answered_at);
  const when = Number.isFinite(answered) ? at(answered) : "";
  // An approval names what was approved, its command in mono, not the button ("Allow"); a prompt or a
  // question answered names the choice: "You chose · Yes" (soft-final SPEC §11 Request card, answered).
  // A terminal prompt's `asked` is its screen text run together, so only an approval's is a command.
  const approval = record.kind === "approval" && (record.outcome === "approved" || record.outcome === "denied");
  const chose = record.outcome === "approved" || record.outcome === "denied" || record.outcome === "answered";
  const verb = approval || !chose ? VERB[record.outcome] : "You chose";
  const command = approval ? record.asked.split(/\r?\n/).find((line) => line.trim())?.trim() : undefined;
  const answer = command || record.answer || (SCREEN_PROMPTS.has(record.kind) ? record.title : record.asked) || record.title || verb;
  return {
    key: answeredKeys(record)[0] ?? `answered:${record.id}`,
    verb,
    answer,
    // A request that passed unanswered shows what it asked; a command among it is in mono (SPEC §1.1).
    mono: Boolean(command) || (record.outcome === "not_answered" && looksLikeCommand(answer)),
    title: record.asked || record.title || "",
    source: [answeredBy(record), when].filter(Boolean).join(" · "),
    brain: Boolean(record.thread_id || record.work_id),
    workerId: record.session_id,
  };
}
