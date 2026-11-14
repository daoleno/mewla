/**
 * The Workers of a Work (SPEC §1.2 Work card, §11 Work detail; daemon WIRE §1): every Session that
 * joined it, oldest first. A live one reads its Session as the list has it now (state, what it does,
 * its phase); a finished one, the snapshot's row.
 */
import { elapsedShort, readableSummary } from "../sessions/activity";
import type { TrayRow } from "../tray/model";
import { listTime, sessionRowModel, withoutSessionId } from "../sessions/listRow";
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
  /** "Claude Code": the row's title when the Worker has no name of its own. */
  agentLabel: string;
  state: WorkWorkerState;
  /** What it does now, or its result line. */
  summary: string;
  /** A state the row must say in words: "Needs you", "Failed", "Ended"; none while it runs or once it is done. */
  stateLabel?: string;
  /** "live" while it works; else when it finished. */
  time: string;
  /** When its current turn began (ms), else when it joined the Work: a live row's end says how long it has worked. */
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

/**
 * A live Worker's state. Blocked needs you only on what Brain cannot answer, a prompt or an approval on its
 * terminal; blocked on its own report (`--attention user_input`) it waits on Brain, so it still works for the
 * Work (owner 2026-11-13, shot 11).
 */
function liveState(session: Worker): WorkWorkerState | undefined {
  if (session.status === "blocked" && session.delegated && !session.prompt && !session.approvals?.length) return "running";
  return LIVE_STATE[session.status];
}

/**
 * A Worker's name without the Session id the daemon appends ("Hello writer (mh-17b3…)" → "Hello writer"):
 * the id is no name to show. Only that exact suffix goes; "Fix (macOS)" stays as it is.
 */
export function workerName(name: string | undefined): string {
  return withoutSessionId(name);
}

export function workWorkerLines(rows: readonly WorkWorkerSession[] | undefined, sessions: ReadonlyMap<string, Worker>, now = new Date()): WorkWorkerLine[] {
  return (rows ?? []).map((row) => {
    const session = sessions.get(row.session_id);
    const model = session ? sessionRowModel(session) : null;
    const following = row.live && session;
    const state = following ? liveState(session) ?? row.state : row.state;
    const working = state === "running";
    // What it reported (a live Session's summary only once it reported progress: before, it is the daemon
    // reading the screen); the terminal's footer ("⏵⏵ bypass permissions on…") is never a summary.
    const reported = following && Boolean(session.phase?.trim() || session.event_kind?.trim());
    const summary = (reported ? readableSummary(session.summary) : "") || readableSummary(row.summary);
    const joined = Date.parse(row.joined_at);
    // A working Session's time counts from its current turn (SPEC §11 Elapsed time), as its tray row and Working line do.
    const turn = following && working && session.running_since ? session.running_since : NaN;
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
      time: working ? "live" : listTime(Number.isFinite(at) ? at : undefined, now),
      since: Number.isFinite(turn) ? turn : Number.isFinite(joined) ? joined : undefined,
      openable: Boolean(session),
      project: projectName(session?.cwd) || undefined,
    };
  });
}

/** Whether two lists of a Work's worker lines say the same: a card made anew for an equal list draws nothing new. */
export function sameWorkWorkerLines(a: readonly WorkWorkerLine[], b: readonly WorkWorkerLine[]): boolean {
  if (a === b) return true;
  if (a.length !== b.length) return false;
  return a.every((line, index) => {
    const other = b[index]!;
    const keys = Object.keys(line) as (keyof WorkWorkerLine)[];
    return keys.length === Object.keys(other).length && keys.every((key) => line[key] === other[key]);
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

/**
 * A Worker's row on Work detail (SPEC §11 Work detail, Work card): the logo names the agent, so the title is the
 * Worker's task name alone ("Quickstart rewrite"), the agent only when it has none; the line is what it does; the
 * end is its state as a tray row ends: "● 10m" while it works, a red dot and "Needs you" while it waits on you,
 * else when it finished. Failed and Ended say so in the line.
 */
export function workDetailWorkerRow(worker: WorkWorkerLine, now: number, workProject?: string): TrayRow {
  const live = worker.state === "running";
  const needs = worker.state === "blocked";
  const named = worker.name && worker.name !== "Worker";
  const said = needs ? undefined : worker.stateLabel;
  return {
    key: `worker:${worker.sessionId}`,
    title: named ? worker.name : worker.agentLabel,
    // The project only where the page's eyebrow ("Work · mewla") does not already say it.
    line: [said, worker.summary || (worker.project !== workProject ? worker.project : "")].filter(Boolean).join(" · "),
    end: live ? elapsedShort(worker.since, now) : needs ? "Needs you" : worker.time,
    live: live || undefined,
    needs: needs || undefined,
    tile: { kind: "agent", agent: worker.agent, flavor: worker.flavor },
    target: { open: "session", workerId: worker.sessionId },
    done: worker.state === "done" || undefined,
  };
}
