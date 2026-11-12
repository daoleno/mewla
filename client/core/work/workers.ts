/**
 * The Workers of a Work (SPEC §1.2 Work card, §11 Work detail; daemon WIRE §1): every Session that
 * joined it, oldest first. A live one reads its Session as the list has it now (state, what it does,
 * its phase); a finished one, the snapshot's row.
 */
import { readableSummary } from "../sessions/activity";
import { listTime, sessionRowModel, withoutSessionId } from "../sessions/listRow";
import { progressSteps } from "../sessions/progress";
import type { TerminalFlavor } from "../sessions/terminalFlavor";
import { agentKindFromProvider, typeLabel, type AgentKind } from "../sessions/workerPresentation";
import type { Worker } from "../sessions/workers";
import { projectName } from "./surface";
import type { WorkWorkerSession, WorkWorkerState } from "./types";

export interface WorkWorkerLine {
  sessionId: string;
  /** Its name, else its executor, else "Worker". */
  name: string;
  agent: AgentKind;
  flavor: TerminalFlavor;
  /** "Claude Code": Work detail's rows read "Agent · name". */
  agentLabel: string;
  state: WorkWorkerState;
  /** What it does now, or its result line. */
  summary: string;
  /** A state the row must say in words: "Needs you", "Failed", "Ended"; none while it runs or once it is done. */
  stateLabel?: string;
  /** Running with a reported phase: the filled segments. */
  progress?: number;
  /** "live" while it works; else when it finished. */
  time: string;
  /** When it joined the Work (ms): a live row's end says how long it has worked on it. */
  since?: number;
  /** Its Session is in the list: the line pushes it. */
  openable: boolean;
  /** Its Session's project ("perpetuo"), while the list has the Session. */
  project?: string;
}

/** The card shows this many; the rest are "N more". */
export const WORK_CARD_WORKERS = 3;

const STATE_LABEL: Partial<Record<WorkWorkerState, string>> = { blocked: "Needs you", failed: "Failed", ended: "Ended" };

const LIVE_STATE: Partial<Record<Worker["status"], WorkWorkerState>> = { running: "running", blocked: "blocked", done: "done", failed: "failed" };

export function workWorkerLines(rows: readonly WorkWorkerSession[] | undefined, sessions: ReadonlyMap<string, Worker>, now = new Date()): WorkWorkerLine[] {
  return (rows ?? []).map((row) => {
    const session = sessions.get(row.session_id);
    const model = session ? sessionRowModel(session) : null;
    const following = row.live && session;
    const state = following ? LIVE_STATE[session.status] ?? row.state : row.state;
    const working = state === "running";
    // What it reported (a live Session's summary only once it reported progress: before, it is the daemon
    // reading the screen); the terminal's footer ("⏵⏵ bypass permissions on…") is never a summary.
    const reported = following && Boolean(session.phase?.trim() || session.event_kind?.trim());
    const summary = (reported ? readableSummary(session.summary) : "") || readableSummary(row.summary);
    const joined = Date.parse(row.joined_at);
    const at = Date.parse(working ? row.updated_at : row.ended_at ?? row.updated_at);
    const agent = model?.kind ?? agentKindFromProvider(row.provider);
    const flavor = model?.flavor ?? "shell";
    return {
      sessionId: row.session_id,
      name: withoutSessionId(row.name) || model?.title || row.executor_id || "Worker",
      agent,
      flavor,
      agentLabel: typeLabel(agent, flavor),
      state,
      summary,
      stateLabel: STATE_LABEL[state],
      progress: working ? progressSteps(following ? session.phase ?? row.phase : row.phase) ?? undefined : undefined,
      time: working ? "live" : listTime(Number.isFinite(at) ? at : undefined, now),
      since: Number.isFinite(joined) ? joined : undefined,
      openable: Boolean(session),
      project: projectName(session?.cwd) || undefined,
    };
  });
}

/** The Sessions that work for a Work now: the tray shows the Work's row, not theirs. */
export function liveWorkerIds(rows: readonly WorkWorkerSession[] | undefined): string[] {
  return (rows ?? []).filter((row) => row.live).map((row) => row.session_id);
}

/**
 * The project a Work is about, for Work detail's eyebrow (SPEC §11 "Work · {project}"): its live Session's,
 * else the first of its Workers whose Session the list has; none when nothing says.
 */
export function workProject(slipProject: string | undefined, lines: readonly WorkWorkerLine[]): string | undefined {
  return slipProject || lines.find((line) => line.project)?.project;
}
