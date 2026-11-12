/**
 * Home's state control and tray (soft-final SPEC §1.1–§1.3, §3.2): what waits for you, what runs,
 * today, what is idle and what is done, all from one pure model so the pill, the tray's title and
 * its rows always agree. The count counts the rows you can open, not Workers (§1.2 "The count").
 */
import { progressSteps } from "../sessions/progress";
import type { BrainHostProblem, BrainSurface, BrainWorkerRef } from "../brain";
import { sessionRowModel } from "../sessions/listRow";
import type { AgentKind, TerminalFlavor } from "../sessions/workerPresentation";
import type { Worker, WorkerApproval, WorkerPrompt } from "../sessions/workers";
import { liveWorkerIds } from "../work/workers";
import { brainWorkSurface, type BrainWorkSlip } from "../work/surface";
import type { BrainCurrentWork, BrainOpenWork } from "../work/types";

/** The app's connection to the current computer (ui/session `connection`). */
export type TrayConnection = "connected" | "connecting" | "waiting" | "idle";

/** Where a request's card lives, which decides what a tap on its tray row does (§1.1 "Row tap"). */
export type RequestWhere = "brain" | "session" | "work" | "alert" | "device";

/** A one-tap answer: only a plain yes/no approval of one command or permission has one (§1.1 "One-tap case"). */
export type OneTap =
  | { kind: "approval"; workerId: string; approvalId: string; optionId: string }
  | { kind: "prompt"; workerId: string; promptId: string; number: number };

/** One request: anything that waits for the person, one object with one state (§1.1). */
export interface TrayRequest {
  /** Stable while the request is open: the thread card and the tray row share it. */
  key: string;
  kind: "choice" | "prompt" | "approval" | "attention" | "work" | "alert" | "device";
  where: RequestWhere;
  /** The question, or what is asked ("Run git push?"). */
  title: string;
  /** Who asks: "Brain", a Session's title, a Work's title. */
  source: string;
  /** The Session that asks (the Brain host for Brain's own prompts). */
  workerId?: string;
  workId?: string;
  itemId?: string;
  /** A new device's request to be let in. */
  deviceId?: string;
  agent?: AgentKind;
  flavor?: TerminalFlavor;
  /** Approve on the row; absent sends the tap to the card ("Answer"). */
  oneTap?: OneTap;
}

export type TrayTile =
  | { kind: "agent"; agent: AgentKind; flavor: TerminalFlavor }
  /** Brain's mark: a Work with several Workers or none. */
  | { kind: "brain" }
  | { kind: "calendar" }
  | { kind: "device" };

/** What a row opens. */
export type TrayTarget =
  | { open: "session"; workerId: string }
  | { open: "work"; workId: string }
  | { open: "item"; itemId: string; sessionId?: string };

export interface TrayRow {
  key: string;
  title: string;
  line: string;
  /** The end: "live", a time, "" when nothing. */
  end: string;
  tile: TrayTile;
  target: TrayTarget;
  /** An unread result (an ink dot at the end). */
  unread?: boolean;
  /** Done rows: 400-weight title and a check. */
  done?: boolean;
  /** Queued and waiting Work: a quiet row under Running. */
  quiet?: boolean;
  /** Today: already past (dimmed above the now line). */
  past?: boolean;
  /** Segments of the progress bar filled (0–5) from the running Worker's phase; absent: no bar. */
  progress?: number;
}

/** A Calendar item of today, as the caller reads it with the phone's clock. */
export interface TrayTodayInput {
  id: string;
  title: string;
  /** Its wall-clock time on the row ("9:30 AM"). */
  time: string;
  at: number;
  running: boolean;
  /** The Session its newest run works in, when that Session is listed. */
  sessionId?: string;
}

export type StateDot = "seal" | "ink" | "ring" | null;

export type StateKind = "none" | "quiet" | "running" | "needs" | "connecting" | "offline" | "pair" | "starting" | "cant_start";

/** The header's state control (§11 State control). */
export interface StateControlModel {
  kind: StateKind;
  words: string;
  dot: StateDot;
  /** Ink text, else soft. */
  ink: boolean;
  /** Pair again has no chevron: its tap goes straight to pairing. */
  chevron: boolean;
}

export type TrayNoticeAction = "try_again" | "details" | "pair_again" | "use_suggested" | "switch_executor";

/** The tray's first group when something is wrong (§11 Tray notice). */
export interface TrayNotice {
  kind: "offline" | "pair" | "cant_start";
  title: string;
  detail: string;
  /** Ink first. */
  actions: { id: TrayNoticeAction; label: string; executor?: string }[];
}

export type TraySort = "opened" | "activity";

export interface TrayInput {
  hasServer: boolean;
  computerName: string;
  connection: TrayConnection;
  /** The phone found the computer forgot it (ui/more/settings/computers `currentIssue()`). */
  pairAgain: boolean;
  /** Why it can't connect, as the diagnosis names it ("Relay unreachable"), with when it was seen. */
  diagnosis?: string;
  surface: BrainSurface;
  hostProblem?: BrainHostProblem;
  /** The current computer's Sessions; Brain's host is never among them. */
  workers: readonly Worker[];
  /** Brain's own host Session, for the prompts it is blocked on. */
  brainHost?: { id: string; prompt?: WorkerPrompt } | null;
  /** The question Brain asked in its chat that is still pending (AskUserQuestion). */
  brainChoice?: { callId: string; question: string } | null;
  currentWork?: readonly BrainCurrentWork[];
  openWork?: readonly BrainOpenWork[];
  today: readonly TrayTodayInput[];
  /** Calendar alerts nobody answered (core/calendar/alerts `openAlerts`). */
  alerts: readonly { id: string; title: string }[];
  /** New devices asking the current computer to let them in (ui/enrollment `deviceRequests`). */
  devices?: readonly { id: string; name: string }[];
  /** Requests answered here that the computer has not cleared yet: sealed everywhere at once (§1.1). */
  answered?: ReadonlySet<string>;
  /** When each Session was last opened here (core/sessions/deviceState `getRecentWorkerOpens`). */
  opened: Readonly<Record<string, number>>;
  sort?: TraySort;
  now: number;
}

export interface TrayModel {
  state: StateControlModel;
  /** The tray's title: the same words as the state control. */
  title: string;
  notice: TrayNotice | null;
  /** The lists are last known: dimmed under the notice (offline, connecting, pair again). */
  dimmed: boolean;
  needs: TrayRequest[];
  running: TrayRow[];
  today: TrayRow[];
  /** More of today than the three shown: the "Calendar ›" link says so. */
  todayCount: number;
  idle: TrayRow[];
  /** Done, unread results first (the tray shows three, "All ›" opens the rest). */
  done: TrayRow[];
}

/** Running rows shown before "N more". */
export const RUNNING_SHOWN = 5;
/** Today's rows shown before "Calendar ›". */
export const TODAY_SHOWN = 3;
/** Done rows shown before "All ›". */
export const DONE_SHOWN = 3;

const NEGATIVE = /^(?:no|deny|reject|cancel|don'?t)\b/i;
const ALWAYS = /\b(?:always|don'?t ask|for this session|for all|remember)\b/i;

function isQuestionApproval(approval: WorkerApproval): boolean {
  return approval.tool?.trim().toLowerCase() === "askuserquestion";
}

/** Approve on the row only for a plain yes/no of one command or permission. */
function approvalOneTap(workerId: string, approval: WorkerApproval): OneTap | undefined {
  if (approval.options.length !== 2) return undefined;
  const allow = approval.options.find((option) => option.scope === "once");
  const deny = approval.options.find((option) => option.scope === "deny");
  if (!allow || !deny || ALWAYS.test(allow.label)) return undefined;
  return { kind: "approval", workerId, approvalId: approval.id, optionId: allow.id };
}

function promptOneTap(workerId: string, prompt: WorkerPrompt): OneTap | undefined {
  if (prompt.kind === "choice" || prompt.kind === "plan") return undefined;
  const options = prompt.options ?? [];
  if (options.length !== 2) return undefined;
  const yes = options.find((option) => !NEGATIVE.test(option.label.trim()));
  const no = options.find((option) => NEGATIVE.test(option.label.trim()));
  if (!yes || !no || ALWAYS.test(yes.label)) return undefined;
  // A default on the negative answer is a destructive default: go to the card.
  if (no.selected) return undefined;
  return { kind: "prompt", workerId, promptId: prompt.id, number: yes.number };
}

function promptTitle(prompt: WorkerPrompt): string {
  if (prompt.kind === "choice") return prompt.questions?.find((q) => !q.answered)?.question || prompt.title || "A question";
  const request = prompt.request?.split(/\r?\n/).map((line) => line.trim()).find(Boolean);
  return request || prompt.title || "Waiting for confirmation";
}

function approvalTitle(approval: WorkerApproval): string {
  return approval.summary?.split(/\r?\n/)[0]?.trim() || approval.tool || "Waiting for confirmation";
}

function titleOf(worker: Worker): string {
  return sessionRowModel(worker).title || worker.name || "Session";
}

/**
 * The open requests, oldest first as they arrived: Brain's own (its chat's question, the prompt its
 * Session is blocked on), each Session's (its reported decisions, else the prompt on its terminal,
 * else that it waits on you), the Work that waits on a decision and the unanswered calendar alerts.
 * A request of a Session that works for Brain's delegated Work shows in Brain's thread too, so its
 * row leads there; one of a Session the person started is only in that Session.
 */
export function requestsOf(input: Pick<TrayInput, "workers" | "brainHost" | "brainChoice" | "currentWork" | "alerts" | "devices" | "answered" | "now">): TrayRequest[] {
  const out: TrayRequest[] = [];
  if (input.brainChoice) {
    out.push({ key: `brain-choice:${input.brainChoice.callId}`, kind: "choice", where: "brain", title: input.brainChoice.question || "Brain asks", source: "Brain", workerId: input.brainHost?.id });
  }
  const host = input.brainHost;
  if (host?.prompt && !(host.prompt.kind === "choice" && input.brainChoice && host.prompt.call_id === input.brainChoice.callId)) {
    out.push({ key: `prompt:${host.id}:${host.prompt.id}`, kind: "prompt", where: "brain", title: promptTitle(host.prompt), source: "Brain", workerId: host.id, oneTap: promptOneTap(host.id, host.prompt) });
  }
  for (const worker of input.workers) {
    const where: RequestWhere = worker.delegated ? "brain" : "session";
    const model = sessionRowModel(worker);
    const base = { where, source: model.title || worker.name || "Session", workerId: worker.id, agent: model.kind, flavor: model.flavor };
    const decisions = (worker.approvals ?? []).filter((approval) => !isQuestionApproval(approval));
    if (decisions.length > 0) {
      for (const approval of decisions) out.push({ ...base, key: `approval:${worker.id}:${approval.id}`, kind: "approval", title: approvalTitle(approval), oneTap: approvalOneTap(worker.id, approval) });
    } else if (worker.prompt) {
      out.push({ ...base, key: `prompt:${worker.id}:${worker.prompt.id}`, kind: "prompt", title: promptTitle(worker.prompt), oneTap: promptOneTap(worker.id, worker.prompt) });
    } else if (worker.needs_attention) {
      // Blocked on something the computer could not read: its card says "Open terminal".
      out.push({ ...base, key: `attention:${worker.id}`, kind: "attention", title: worker.summary?.trim() || "Waiting for you" });
    }
  }
  for (const slip of brainWorkSurface(input.currentWork, workerRefs(input.workers), input.now).slips) {
    if (slip.group !== "needs") continue;
    out.push({ key: `work:${slip.workId}`, kind: "work", where: "work", title: slip.question || slip.title, source: slip.title, workId: slip.workId });
  }
  for (const alert of input.alerts) out.push({ key: `alert:${alert.id}`, kind: "alert", where: "alert", title: alert.title, source: "Calendar", itemId: alert.id });
  for (const device of input.devices ?? []) out.push({ key: `device:${device.id}`, kind: "device", where: "device", title: "Approve new device", source: device.name, deviceId: device.id });
  const answered = input.answered;
  return answered?.size ? out.filter((request) => !answered.has(request.key)) : out;
}

/** The Sessions as Brain's Work names them, so a Work finds the live Session that runs it. */
function workerRefs(workers: readonly Worker[]): BrainWorkerRef[] {
  return workers.map((worker) => ({ id: worker.id, name: worker.name, status: worker.status, phase: worker.phase, cwd: worker.cwd, command: worker.command, provider: worker.provider }));
}

function hasRequest(worker: Worker): boolean {
  return Boolean(worker.needs_attention || worker.prompt || (worker.approvals ?? []).some((approval) => !isQuestionApproval(approval)));
}

function sessionRow(worker: Worker): TrayRow {
  const model = sessionRowModel(worker);
  const ended = worker.status === "done" || worker.status === "failed";
  return {
    key: `session:${worker.id}`,
    title: model.title || worker.name || "Session",
    line: `${model.previewPrefix ?? ""}${model.preview}`,
    end: model.time === "live" ? "live" : model.time,
    tile: { kind: "agent", agent: model.kind, flavor: model.flavor },
    target: { open: "session", workerId: worker.id },
    done: ended || undefined,
    progress: ended ? undefined : progressSteps(worker.phase) ?? undefined,
  };
}

function workRow(slip: BrainWorkSlip, sessions: ReadonlyMap<string, Worker>): TrayRow {
  const joined = slip.workerSessions ?? [];
  // A Work with several Workers is one row with Brain's mark, "N Workers", opening Work detail (§1.3).
  const several = joined.length > 1;
  const sessionId = several ? undefined : joined[0]?.session_id ?? slip.sessionId;
  const session = sessionId ? sessions.get(sessionId) : undefined;
  const model = session ? sessionRowModel(session) : null;
  const done = slip.group === "back";
  return {
    key: `work:${slip.workId}`,
    title: slip.title,
    line: several ? `${joined.length} Workers` : done ? slip.summary || slip.statusLabel : slip.phase || slip.waitFor || slip.who || slip.statusLabel,
    end: slip.group === "running" ? "live" : "",
    // A Work its one Worker runs opens that Session (its logo); with none, Work detail (Brain's mark).
    tile: model ? { kind: "agent", agent: model.kind, flavor: model.flavor } : { kind: "brain" },
    target: session ? { open: "session", workerId: session.id } : { open: "work", workId: slip.workId },
    unread: done && slip.unread ? true : undefined,
    done: done || undefined,
    quiet: slip.group === "waiting" || slip.group === "queued" || undefined,
    progress: slip.group === "running" ? progressSteps(session?.phase) ?? undefined : undefined,
  };
}

function stateOf(input: TrayInput, needs: number, running: number): StateControlModel {
  const s = (kind: StateKind, words: string, dot: StateDot, ink: boolean, chevron = true): StateControlModel => ({ kind, words, dot, ink, chevron });
  if (!input.hasServer) return s("none", "", null, false, false);
  if (input.pairAgain) return s("pair", "Pair again", "seal", true, false);
  if (input.connection === "connecting") return s("connecting", "Connecting…", "ring", false);
  if (input.connection !== "connected") return s("offline", "Offline", "ring", false);
  if (input.surface === "host_down") return s("cant_start", "Can't start", "seal", true);
  // What waits on you outranks Brain still starting: a Session's request can be answered meanwhile.
  if (needs > 0) return s("needs", needs === 1 ? "1 needs you" : `${needs} need you`, "seal", true);
  if (input.surface === "waking") return s("starting", "Starting…", "ring", false);
  if (running > 0) return s("running", `${running} running`, "ink", true);
  return s("quiet", "All quiet", null, false);
}

function noticeOf(input: TrayInput): TrayNotice | null {
  if (!input.hasServer) return null;
  const name = input.computerName || "Your computer";
  if (input.pairAgain) return { kind: "pair", title: "Device not paired", detail: `${name} forgot this phone.`, actions: [{ id: "pair_again", label: "Pair again" }] };
  if (input.connection === "waiting" || input.connection === "idle") {
    return { kind: "offline", title: `${name} is offline`, detail: input.diagnosis || "Can't reach it", actions: [{ id: "try_again", label: "Try again" }, { id: "details", label: "Details" }] };
  }
  if (input.connection === "connected" && input.surface === "host_down") {
    const suggested = input.hostProblem?.suggest_executor;
    return {
      kind: "cant_start",
      title: "Brain can't start",
      detail: input.hostProblem?.detail || "",
      actions: [...(suggested ? [{ id: "use_suggested" as const, label: `Use ${suggested}`, executor: suggested }] : []), { id: "switch_executor", label: "Switch executor" }],
    };
  }
  return null;
}

export function trayModel(input: TrayInput): TrayModel {
  const needs = requestsOf(input);
  const sessions = new Map(input.workers.map((worker) => [worker.id, worker] as const));
  // A Session that runs a listed Work is that Work's row; asking you, it is a Needs you row only, and
  // the Work has no row of its own until it is answered.
  const asking = new Set(input.workers.filter(hasRequest).map((worker) => worker.id));
  const slips = brainWorkSurface(input.currentWork, workerRefs(input.workers), input.now, input.openWork).slips
    .filter((slip) => slip.group === "needs" || ![slip.sessionId, ...liveWorkerIds(slip.workerSessions)].some((id) => id && asking.has(id)));
  const folded = new Set(slips.flatMap((slip) => [slip.sessionId, ...liveWorkerIds(slip.workerSessions)]).filter((id): id is string => Boolean(id)));
  const opened = (id: string) => input.opened[id] ?? 0;
  const activity = (worker: Worker) => worker.updated_at ?? worker.started_at ?? 0;
  const order = (a: Worker, b: Worker) =>
    input.sort === "activity" ? activity(b) - activity(a) : opened(b.id) - opened(a.id) || activity(b) - activity(a);

  const live = input.workers.filter((worker) => !folded.has(worker.id) && !hasRequest(worker));
  const runningSessions = live.filter((worker) => worker.status === "running").sort(order).map(sessionRow);
  const runningWork = slips.filter((slip) => slip.group === "running").map((slip) => workRow(slip, sessions));
  const quietWork = slips.filter((slip) => slip.group === "waiting" || slip.group === "queued").map((slip) => workRow(slip, sessions));
  const running = [...runningSessions, ...runningWork, ...quietWork];

  const idle = live.filter((worker) => worker.status === "blocked" || worker.status === "unknown").sort(order).map(sessionRow);
  const doneWork = slips.filter((slip) => slip.group === "back").map((slip) => workRow(slip, sessions));
  const ended = live.filter((worker) => worker.status === "done" || worker.status === "failed").sort((a, b) => activity(b) - activity(a)).map(sessionRow);
  const done = [...doneWork.filter((row) => row.unread), ...doneWork.filter((row) => !row.unread), ...ended];

  const todayRows: TrayRow[] = [...input.today]
    .sort((a, b) => a.at - b.at)
    .map((item) => ({
      key: `today:${item.id}`,
      title: item.title,
      line: item.running ? "Running" : item.time,
      end: item.running ? "live" : item.time,
      tile: { kind: "calendar" as const },
      target: item.running && item.sessionId ? { open: "session" as const, workerId: item.sessionId } : { open: "item" as const, itemId: item.id, sessionId: item.sessionId },
      past: !item.running && item.at < input.now ? true : undefined,
    }));
  // The next three: running and still to come, before what has passed.
  const upcoming = todayRows.filter((row) => !row.past);
  const today = (upcoming.length ? upcoming : todayRows).slice(0, TODAY_SHOWN);

  const state = stateOf(input, needs.length, running.filter((row) => !row.quiet).length);
  return {
    state,
    title: state.words || "Mewla",
    notice: noticeOf(input),
    dimmed: input.hasServer && (input.pairAgain || input.connection !== "connected"),
    needs,
    running,
    today,
    todayCount: todayRows.length,
    idle,
    done,
  };
}
