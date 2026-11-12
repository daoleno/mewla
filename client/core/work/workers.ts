/**
 * The Workers of a Work (SPEC §1.2 Work card, §11 Work detail; daemon WIRE §1): every Session that
 * joined it, oldest first. A live one reads its Session as the list has it now (state, what it does,
 * its phase); a finished one, the snapshot's row.
 */
import { listTime, sessionRowModel } from "../sessions/listRow";
import { progressSteps } from "../sessions/progress";
import type { TerminalFlavor } from "../sessions/terminalFlavor";
import { agentKindFromProvider, type AgentKind } from "../sessions/workerPresentation";
import type { Worker } from "../sessions/workers";
import type { WorkWorkerSession, WorkWorkerState } from "./types";

export interface WorkWorkerLine {
  sessionId: string;
  /** Its name, else its executor, else "Worker". */
  name: string;
  agent: AgentKind;
  flavor: TerminalFlavor;
  state: WorkWorkerState;
  /** What it does now, or its result line. */
  summary: string;
  /** Running with a reported phase: the filled segments. */
  progress?: number;
  /** "live" while it works; else when it finished. */
  time: string;
  /** Its Session is in the list: the line pushes it. */
  openable: boolean;
}

/** The card shows this many; the rest are "N more". */
export const WORK_CARD_WORKERS = 3;

const LIVE_STATE: Partial<Record<Worker["status"], WorkWorkerState>> = { running: "running", blocked: "blocked", done: "done", failed: "failed" };

export function workWorkerLines(rows: readonly WorkWorkerSession[] | undefined, sessions: ReadonlyMap<string, Worker>, now = new Date()): WorkWorkerLine[] {
  return (rows ?? []).map((row) => {
    const session = sessions.get(row.session_id);
    const model = session ? sessionRowModel(session) : null;
    const following = row.live && session;
    const state = following ? LIVE_STATE[session.status] ?? row.state : row.state;
    const working = state === "running";
    const summary = (following ? session.summary?.trim() || row.summary : row.summary)?.replace(/\s+/g, " ").trim() ?? "";
    const at = Date.parse(working ? row.updated_at : row.ended_at ?? row.updated_at);
    return {
      sessionId: row.session_id,
      name: row.name?.trim() || model?.title || row.executor_id || "Worker",
      agent: model?.kind ?? agentKindFromProvider(row.provider),
      flavor: model?.flavor ?? "shell",
      state,
      summary,
      progress: working ? progressSteps(following ? session.phase ?? row.phase : row.phase) ?? undefined : undefined,
      time: working ? "live" : listTime(Number.isFinite(at) ? at : undefined, now),
      openable: Boolean(session),
    };
  });
}

/** The Sessions that work for a Work now: the tray shows the Work's row, not theirs. */
export function liveWorkerIds(rows: readonly WorkWorkerSession[] | undefined): string[] {
  return (rows ?? []).filter((row) => row.live).map((row) => row.session_id);
}
