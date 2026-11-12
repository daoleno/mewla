/**
 * What a Session is doing, in words, for its tray row's line (SPEC §11 Tray row: "the line 15/20 soft").
 * Never the terminal's own text: `last_output_lines` is the provider's screen (its status footer, the
 * prompt Brain pasted in), so it is never read here.
 */
import type { Worker, WorkerApproval, WorkerPrompt } from "./workers";

/**
 * A provider's status footer as the daemon read it off the terminal ("⏵⏵ bypass permissions on (shift+tab
 * to cycle)", "⏸ manual mode on · ? for shortcuts"): the screen's chrome, never what the thing is doing.
 * Claude Sessions on current daemons report it as their summary.
 */
export function isTerminalFooter(text: string | undefined): boolean {
  const line = text?.trim() ?? "";
  if (!line) return false;
  return /^[⏵⏸⏴⏯▶►]/u.test(line) || /shift\s*\+\s*tab to cycle|\?\s*for shortcuts/i.test(line);
}

/** A summary as a row shows it: one line, and nothing when it is the terminal's footer. */
export function readableSummary(text: string | undefined): string {
  const line = text?.replace(/\s+/g, " ").trim() ?? "";
  return line && !isTerminalFooter(line) ? line : "";
}

/**
 * The words `sessionActivity` falls back to that only name the state: "Working" under Running, "Idle" under
 * Idle, "Done" under Done. They repeat the row's section and its dot, so a row says something else instead.
 * A phase ("Verifying"), "Failed" and what it asks you say something, so they are not here.
 */
export const BARE_STATE_WORDS: ReadonlySet<string> = new Set(["Working", "Idle", "Waiting", "Done"]);

/** An AskUserQuestion "approval" is a question in the chat, not a decision. */
export function isQuestionApproval(approval: WorkerApproval): boolean {
  return approval.tool?.trim().toLowerCase() === "askuserquestion";
}

/** A prompt line that says nothing about this request: the agent's tips and its "requires approval" boilerplate. */
export const isPromptNoise = (line: string) => /^tip:/i.test(line) || /^this command requires approval\.?$/i.test(line);

/** What a terminal prompt asks: the open question, else the request's first line past tips, else its title. */
export function promptTitle(prompt: WorkerPrompt): string {
  if (prompt.kind === "choice") return prompt.questions?.find((q) => !q.answered)?.question || prompt.title || "A question";
  const request = prompt.request?.split(/\r?\n/).map((line) => line.trim()).find((line) => line && !isPromptNoise(line));
  return request || prompt.title || "Waiting for confirmation";
}

/** What a reported decision asks: its summary's first line, else its tool. */
export function approvalTitle(approval: WorkerApproval): string {
  return approval.summary?.split(/\r?\n/)[0]?.trim() || approval.tool || "Waiting for confirmation";
}

/** A reported phase as the line says it while nothing else is reported. */
const PHASE_WORDS: Record<string, string> = {
  starting: "Starting",
  reading: "Reading",
  planning: "Planning",
  working: "Working",
  verifying: "Verifying",
  reporting: "Reporting",
};

/**
 * The line, in order: what it asks you (a decision, the prompt on its terminal, "Waiting for you"); what it
 * reported through `mewla worker progress` (a summary sent with a phase or an event kind, while it runs or
 * once it is done); else its state in a word ("Working", "Verifying", "Idle", "Done"). A summary without a
 * report is the daemon reading the terminal, and a blocked, failed or idle Session's summary is the screen's
 * last line even after a report, so those fall to the word.
 */
export function sessionActivity(worker: Pick<Worker, "status" | "summary" | "phase" | "event_kind" | "needs_attention" | "prompt" | "approvals">): string {
  const decision = (worker.approvals ?? []).find((approval) => !isQuestionApproval(approval));
  if (decision) return approvalTitle(decision);
  if (worker.prompt) return promptTitle(worker.prompt);
  if (worker.needs_attention) return "Waiting for you";
  const reported = Boolean(worker.event_kind?.trim() || worker.phase?.trim()) && (worker.status === "running" || worker.status === "done");
  const summary = reported ? readableSummary(worker.summary) : "";
  if (summary) return summary;
  switch (worker.status) {
    case "running": return PHASE_WORDS[worker.phase?.trim().toLowerCase() ?? ""] ?? "Working";
    case "blocked": return "Waiting";
    case "done": return "Done";
    case "failed": return "Failed";
    default: return "Idle";
  }
}

/** How long something has run, for the row's end: "1m" at least, then "12m", "3h", "2d". Empty without a start. */
export function elapsedShort(since: number | undefined, now: number): string {
  if (!since || !Number.isFinite(since)) return "";
  const minutes = Math.max(1, Math.floor((now - since) / 60_000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h` : `${Math.floor(hours / 24)}d`;
}
