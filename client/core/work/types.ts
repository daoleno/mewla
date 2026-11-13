/**
 * Brain's Work as `brain_snapshot` carries it (daemon brain/lifecycle.go CurrentWork):
 * what Brain is doing for you, each with where it stands. Not the Calendar's items, and
 * not the Markdown Work files (`workItems.ts`).
 */
export type BrainWorkStatus =
  | "open"
  | "running"
  | "waiting"
  | "needs_input"
  | "done"
  | "cancelled";

export type BrainWorkWake = {
  kind: "session_terminal" | "calendar_result" | "user_input";
  ref: string;
};

export type BrainWorkProgressMode = "owned" | "waiting" | "ready";

export type BrainSessionFinalization = {
  session_id: string;
  delegated: boolean;
  state: "pending" | "failed" | "complete" | "skipped";
  attempts?: number;
  last_error?: string;
  updated_at: string;
};

export type BrainWorkAttentionState = "queued" | "reserved" | "reviewing";

/** A Worker Session that joined a Work (daemon WIRE §1): oldest first, live or finished. */
export type WorkWorkerState = "running" | "blocked" | "done" | "failed" | "ended";

export type WorkWorkerSession = {
  session_id: string;
  /** What it was called when it joined, or its live name; absent when never recorded. */
  name?: string;
  executor_id?: string;
  provider?: string;
  state: WorkWorkerState;
  /** Live: what it does now; finished: its result line. */
  summary?: string;
  /** Live only: the Worker's reported phase. */
  phase?: string;
  /** Open and working on this Work. */
  live: boolean;
  joined_at: string;
  ended_at?: string;
  updated_at: string;
};

/** A Work result's next step (daemon WIRE §4): what the result truthfully offers. */
export type ResultAction =
  | { kind: "pull_request"; url: string }
  | { kind: "git_review"; session_id: string; cwd: string }
  /** A file of the Worker's project, `path` relative to `cwd`. */
  | { kind: "file"; session_id: string; cwd: string; path: string }
  /** Brain's workspace, relative to it. */
  | { kind: "brain_file"; path: string }
  | { kind: "brain_folder"; path: string };

/** `brain_work_detail`'s Work: one Work's page, current or closed. */
export type BrainWorkDetail = {
  work_id: string;
  revision: number;
  title: string;
  /** The Work's brief, whole (only the detail carries it); daemons before it omit it. */
  objective?: string;
  status: BrainWorkStatus;
  created_at: string;
  updated_at: string;
  worker_sessions: WorkWorkerSession[];
  /** Its newest result's next steps. */
  result_actions?: ResultAction[];
};

export type BrainCurrentWork = {
  work_id: string;
  revision: number;
  title: string;
  status: BrainWorkStatus;
  progress_mode?: BrainWorkProgressMode;
  attempt_session_id?: string;
  attempt_delegated?: boolean;
  wait_for?: string;
  wake?: BrainWorkWake;
  attention_state?: BrainWorkAttentionState;
  session_finalizations?: BrainSessionFinalization[];
  unread_result: boolean;
  /** One line about the newest outcome; daemons before 0.2.1 omit it. */
  summary?: string;
  updated_at?: string;
  /** What Brain asks you about this Work, with one-tap answers. */
  question?: string;
  choices?: string[];
  /** The choice Brain recommends, one of `choices` (WIRE §10): its button is tagged "Brain's pick". */
  recommended?: string;
  /** When the Work started waiting on a decision; never moves while it waits. */
  attention_since?: string;
  /** The open review's reason, e.g. "turn_lost" for an unknown outcome. */
  attention_reason?: string;
  /** Hidden from Needs you until then; the app compares the time. */
  snoozed_until?: string;
  /** Your newest decision on it, until a newer result comes back. */
  user_action?: BrainWorkUserAction;
  /** The schedule and run a Calendar occurrence's Work came from. */
  calendar?: BrainWorkCalendarRef;
  /** Every Worker Session that joined it; absent until one has. */
  worker_sessions?: WorkWorkerSession[];
};

export type BrainWorkCalendarRef = { item_id: string; run_id: string };

/**
 * Unfinished Work outside current_work: no Worker has it yet, or it waits on
 * something that is not live. The Work rail lists it with its state.
 */
export type BrainOpenWork = {
  work_id: string;
  title: string;
  status: BrainWorkStatus;
  progress_mode?: BrainWorkProgressMode;
  wait_for?: string;
  wake?: BrainWorkWake;
  updated_at?: string;
  calendar?: BrainWorkCalendarRef;
  worker_sessions?: WorkWorkerSession[];
};

export type BrainWorkUserActionKind = "reply" | "close" | "dismiss" | "stop" | "snooze";

export type BrainWorkUserAction = {
  kind: "reply" | "close" | "snooze";
  text?: string;
  at: string;
  /** For a reply: Brain's admission ("accepted", "uncertain"). */
  admission?: string;
};

/** Brain's declared current objective and how much of its Work is back. */
export type BrainObjective = {
  title: string;
  total: number;
  back: number;
};

export type BrainWorkBacklog = {
  total: number;
  queued_attention: number;
  historical_results: number;
};

export type { BrainWorkerRef } from "../brain";
