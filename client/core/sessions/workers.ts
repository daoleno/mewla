/**
 * The Sessions the current computer reports, as the Expo app's store kept them
 * (store/workers.tsx), without React: `workerReducer` is a plain function over
 * a per-server map, and `workerSessionFeedAction` turns an applied feed frame
 * (see workerSessionFeed.ts) into its action.
 *
 * - `worker_session_list` replaces that server's rows (UPSERT_SERVER_WORKERS)
 *   and is the only thing that stamps the list fresh for the current connection.
 * - `worker_session_created` / `_updated` upsert one row (UPSERT_WORKER).
 * - `worker_session_archived` removes the row by `worker_session.id` (REMOVE_WORKER).
 *
 * The connection here is the app's three-state view of it. The NSH Transport
 * has four states (idle, connecting, connected, waiting); the caller decides
 * what idle and waiting are (the Expo app had no such states).
 */
import {
  normalizeWorkerSessionCapabilities,
  type WorkerSessionCapabilities,
} from "../timeline/sessionCapabilities";
import type {
  WorkerPrompt,
  WorkerPromptKind,
  WorkerPromptOption,
  WorkerPromptQuestion,
} from "../timeline/types";
import { makeSessionKey } from "./sessionKeys";
import {
  bumpServerConnectionGeneration,
  isWorkerSessionListFreshForConnection as isWorkerSessionListFreshForConnectionInput,
  stampWorkerSessionListGeneration as stampWorkerSessionListGenerationForServer,
} from "./workerSessionListTransport";

export type { WorkerPrompt, WorkerPromptKind, WorkerPromptOption, WorkerPromptQuestion };

// constants/tokens.ts
export type WorkerStatus = 'running' | 'blocked' | 'done' | 'failed' | 'unknown';

export type WorkerCapabilities = WorkerSessionCapabilities;

export interface Worker {
  key: string;
  id: string;
  serverId: string;
  serverName: string;
  serverUrl: string;
  name: string;
  status: WorkerStatus;
  project?: string;
  cwd?: string;
  command?: string;
  /** The executors.toml entry the daemon launched this Worker from. */
  executor_id?: string;
  /** Adapter family of that executor (claude, codex, cursor, ...); absent for a plain terminal. */
  provider?: string;
  /** Decisions the agent is waiting on; absent when it waits on none. */
  approvals?: WorkerApproval[];
  /** The blocking prompt open in the Session's terminal right now; absent when there is none. */
  prompt?: WorkerPrompt;
  summary: string;
  phase?: string;
  attention?: string;
  task_class?: string;
  event_kind?: string;
  details_json?: string;
  needs_attention?: boolean;
  last_output_lines: string[];
  started_at?: number;
  updated_at?: number;
  /** While `running` by the terminal: when this run began (WIRE §9); absent on a delegated Worker. */
  running_since?: number;
  /** While `running`: the tool call the agent started last ("Bash curl -sI https://example.org"), from its hook. */
  activity?: string;
  /** That call's tool ("Bash") and what it works on ("curl -sI https://example.org"), to word it as its tool row. */
  activity_tool?: string;
  activity_summary?: string;
  process_id?: number;
  delegated?: boolean;
  capabilities?: WorkerCapabilities;
}

/** A decision the agent is waiting on, as the daemon reports it. */
export interface WorkerApproval {
  id: string;
  kind?: string;
  tool?: string;
  summary?: string;
  options: { id: string; label: string; scope?: string }[];
}

/** A decision the agent is waiting on, as the daemon reports it. */
export interface WorkerApproval {
  id: string;
  kind?: string;
  tool?: string;
  summary?: string;
  options: { id: string; label: string; scope?: string }[];
}

/** The app's view of a computer's connection (store/workers.tsx `ConnectionState`); not the Transport's. */
export type WorkerConnectionState = 'offline' | 'connecting' | 'connected';

export interface WorkerSessionsState {
  /** Each server's rows, in the order the list placed them. */
  workersByServer: Record<string, Worker[]>;
  serverConnections: Record<string, WorkerConnectionState>;
  hydratedServers: Record<string, boolean>;
  /**
   * Transport-owned: increments when a server enters `connected`.
   * Used with workerSessionListGenerationByServer to prove the
   * connect snapshot (worker_session_list) arrived for the current WebSocket generation.
   */
  connectionGenerationByServer: Record<string, number>;
  /** Set to connectionGeneration when UPSERT_SERVER_WORKERS arrives while connected. */
  workerSessionListGenerationByServer: Record<string, number>;
}

export type RawWorker = {
  id: string;
  name: string;
  status: WorkerStatus;
  project?: string;
  cwd?: string;
  command?: string;
  executor_id?: string;
  provider?: string;
  approvals?: unknown;
  prompt?: unknown;
  summary?: string;
  phase?: string;
  attention?: string;
  task_class?: string;
  event_kind?: string;
  details_json?: string;
  needs_attention?: boolean;
  last_output_lines?: string[];
  started_at?: string | number | Date;
  updated_at?: string | number | Date;
  running_since?: string | number | Date;
  activity?: string;
  activity_tool?: string;
  activity_summary?: string;
  process_id?: number;
  delegated?: boolean;
  capabilities?: {
    structured_events?: unknown;
    model_profile_managed?: unknown;
    model_profile_active_switch?: unknown;
    message_rewind?: unknown;
  };
};

export type WorkerServerIdentity = {
  serverId: string;
  serverName: string;
  serverUrl: string;
};

export type Action =
  | ({ type: 'UPSERT_SERVER_WORKERS'; workers: RawWorker[] } & WorkerServerIdentity)
  | ({ type: 'UPSERT_WORKER'; worker: RawWorker } & WorkerServerIdentity)
  | { type: 'REMOVE_WORKER'; serverId: string; worker_id: string }
  | { type: 'SET_SERVER_CONNECTION_STATE'; serverId: string; connectionState: WorkerConnectionState }
  | { type: 'REMOVE_SERVER'; serverId: string };

export const initialWorkerState: WorkerSessionsState = {
  workersByServer: {},
  serverConnections: {},
  hydratedServers: {},
  connectionGenerationByServer: {},
  workerSessionListGenerationByServer: {},
};

/**
 * The action for a feed frame the WorkerSessionFeed said to apply, as the Expo
 * app's _layout.tsx dispatched it; null for a frame that is not a change
 * (the heartbeat) or not part of the feed.
 */
export function workerSessionFeedAction(
  frame: { type?: unknown; worker_sessions?: unknown; worker_session?: unknown },
  server: WorkerServerIdentity,
): Action | null {
  switch (frame.type) {
    case 'worker_session_list':
      return {
        type: 'UPSERT_SERVER_WORKERS',
        ...server,
        workers: (Array.isArray(frame.worker_sessions) ? frame.worker_sessions : []) as RawWorker[],
      };
    case 'worker_session_created':
    case 'worker_session_updated':
      if (!frame.worker_session || typeof frame.worker_session !== 'object') return null;
      return { type: 'UPSERT_WORKER', ...server, worker: frame.worker_session as RawWorker };
    case 'worker_session_archived': {
      const id = (frame.worker_session as { id?: unknown } | undefined)?.id;
      return { type: 'REMOVE_WORKER', serverId: server.serverId, worker_id: typeof id === 'string' ? id : '' };
    }
    default:
      return null;
  }
}

/** One server's rows (empty when it has none). */
export function serverWorkers(state: Pick<WorkerSessionsState, 'workersByServer'>, serverId: string): Worker[] {
  return state.workersByServer[serverId] ?? NO_WORKERS;
}

const NO_WORKERS: Worker[] = [];

export function workerReducer(state: WorkerSessionsState, action: Action): WorkerSessionsState {
  switch (action.type) {
    case 'UPSERT_SERVER_WORKERS': {
      const current = serverWorkers(state, action.serverId);
      const previousByKey = new Map(current.map(agent => [agent.key, agent]));
      let agentsChanged = current.length !== action.workers.length;
      const incomingWorkers = action.workers.map((agent) => {
        const normalized = normalizeWorker(agent, action.serverId, action.serverName, action.serverUrl);
        const previous = previousByKey.get(normalized.key);
        if (previous && agentsEqual(previous, normalized)) {
          return previous;
        }
        agentsChanged = true;
        return normalized;
      });
      const nextWorkers = reconcileServerWorkers(current, action.serverId, incomingWorkers);
      if (!agentsChanged && nextWorkers.some((agent, index) => agent !== current[index])) {
        agentsChanged = true;
      }
      const hydratedServers = markServerHydrated(state.hydratedServers, action.serverId);
      const workerSessionListGenerationByServer = stampWorkerSessionListGeneration(
        state,
        action.serverId,
      );

      if (
        !agentsChanged &&
        hydratedServers === state.hydratedServers &&
        workerSessionListGenerationByServer === state.workerSessionListGenerationByServer
      ) {
        return state;
      }

      return {
        ...state,
        workersByServer: agentsChanged
          ? { ...state.workersByServer, [action.serverId]: nextWorkers }
          : state.workersByServer,
        hydratedServers,
        workerSessionListGenerationByServer,
      };
    }
    case 'UPSERT_WORKER': {
      const nextWorker = normalizeWorker(action.worker, action.serverId, action.serverName, action.serverUrl);
      const current = serverWorkers(state, action.serverId);
      const existingIndex = current.findIndex(agent => agent.key === nextWorker.key);
      const existing = existingIndex >= 0 ? current[existingIndex] : undefined;
      const hydratedServers = markServerHydrated(state.hydratedServers, action.serverId);
      if (
        existing &&
        agentsEqual(existing, nextWorker) &&
        hydratedServers === state.hydratedServers
      ) {
        return state;
      }
      return {
        ...state,
        workersByServer: {
          ...state.workersByServer,
          [action.serverId]: existing
            ? current.map(agent => (agent.key === nextWorker.key ? nextWorker : agent))
            : [...current, nextWorker],
        },
        hydratedServers,
      };
    }
    case 'REMOVE_WORKER': {
      const targetKey = makeSessionKey(action.serverId, action.worker_id);
      const current = serverWorkers(state, action.serverId);
      if (!current.some(agent => agent.key === targetKey)) {
        return state;
      }
      return {
        ...state,
        workersByServer: {
          ...state.workersByServer,
          [action.serverId]: current.filter(agent => agent.key !== targetKey),
        },
      };
    }
    case 'SET_SERVER_CONNECTION_STATE':
      if (state.serverConnections[action.serverId] === action.connectionState) {
        return state;
      }
      {
        const previous = state.serverConnections[action.serverId];
        const connectionGenerationByServer = bumpServerConnectionGeneration(
          state.connectionGenerationByServer,
          action.serverId,
          previous,
          action.connectionState,
        );
        return {
          ...state,
          serverConnections: {
            ...state.serverConnections,
            [action.serverId]: action.connectionState,
          },
          connectionGenerationByServer,
        };
      }
    case 'REMOVE_SERVER':
      if (
        !(action.serverId in state.workersByServer) &&
        !(action.serverId in state.serverConnections) &&
        !(action.serverId in state.hydratedServers) &&
        !(action.serverId in state.connectionGenerationByServer) &&
        !(action.serverId in state.workerSessionListGenerationByServer)
      ) {
        return state;
      }
      return {
        ...state,
        workersByServer: withoutServer(state.workersByServer, action.serverId),
        serverConnections: withoutServer(state.serverConnections, action.serverId),
        hydratedServers: withoutServer(state.hydratedServers, action.serverId),
        connectionGenerationByServer: withoutServer(state.connectionGenerationByServer, action.serverId),
        workerSessionListGenerationByServer: withoutServer(
          state.workerSessionListGenerationByServer,
          action.serverId,
        ),
      };
    default:
      return state;
  }
}

function withoutServer<T>(record: Record<string, T>, serverId: string): Record<string, T> {
  return Object.fromEntries(Object.entries(record).filter(([id]) => id !== serverId));
}

export function reconcileServerWorkers(
  currentWorkers: Worker[],
  serverId: string,
  incomingWorkers: Worker[],
): Worker[] {
  const incomingByKey = new Map(incomingWorkers.map(agent => [agent.key, agent]));
  const knownKeys = new Set(
    currentWorkers
      .filter(agent => agent.serverId === serverId)
      .map(agent => agent.key),
  );
  const next = currentWorkers.flatMap(agent => {
    if (agent.serverId !== serverId) return [agent];
    const replacement = incomingByKey.get(agent.key);
    return replacement ? [replacement] : [];
  });

  for (const agent of incomingWorkers) {
    if (!knownKeys.has(agent.key)) next.push(agent);
  }
  return next;
}

export function countWorkersByServer(
  agents: readonly Pick<Worker, 'serverId'>[],
): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const agent of agents) {
    counts[agent.serverId] = (counts[agent.serverId] ?? 0) + 1;
  }
  return counts;
}

function markServerHydrated(
  hydratedServers: WorkerSessionsState['hydratedServers'],
  serverId: string,
): WorkerSessionsState['hydratedServers'] {
  if (hydratedServers[serverId]) {
    return hydratedServers;
  }
  return {
    ...hydratedServers,
    [serverId]: true,
  };
}

/**
 * The connect snapshot (a full worker_session_list) replaces the retained
 * agents for the current WebSocket connection generation, and is the only
 * thing that stamps it. Incremental UPSERT_WORKER must not.
 */
function stampWorkerSessionListGeneration(
  state: WorkerSessionsState,
  serverId: string,
): WorkerSessionsState['workerSessionListGenerationByServer'] {
  return stampWorkerSessionListGenerationForServer({
    connectionState: state.serverConnections[serverId],
    connectionGeneration: state.connectionGenerationByServer[serverId] ?? 0,
    workerSessionListGenerationByServer: state.workerSessionListGenerationByServer,
    serverId,
  });
}

/** True when the connect snapshot arrived for the current connected generation. */
export function isWorkerSessionListFreshForConnection(
  state: Pick<
    WorkerSessionsState,
    | 'serverConnections'
    | 'connectionGenerationByServer'
    | 'workerSessionListGenerationByServer'
  >,
  serverId: string,
): boolean {
  return isWorkerSessionListFreshForConnectionInput({
    connectionState: state.serverConnections[serverId],
    connectionGeneration: state.connectionGenerationByServer[serverId] ?? 0,
    workerSessionListGeneration:
      state.workerSessionListGenerationByServer[serverId] ?? 0,
  });
}

function agentsEqual(left: Worker, right: Worker): boolean {
  return (
    left === right ||
    (
      left.key === right.key &&
      left.id === right.id &&
      left.serverId === right.serverId &&
      left.serverName === right.serverName &&
      left.serverUrl === right.serverUrl &&
      left.name === right.name &&
      left.status === right.status &&
      left.project === right.project &&
      left.cwd === right.cwd &&
      left.command === right.command &&
      left.executor_id === right.executor_id &&
      left.provider === right.provider &&
      approvalsEqual(left.approvals, right.approvals) &&
      promptsEqual(left.prompt, right.prompt) &&
      left.summary === right.summary &&
      left.phase === right.phase &&
      left.attention === right.attention &&
      left.task_class === right.task_class &&
      left.event_kind === right.event_kind &&
      left.details_json === right.details_json &&
      left.needs_attention === right.needs_attention &&
      stringArraysEqual(left.last_output_lines, right.last_output_lines) &&
      left.started_at === right.started_at &&
      left.updated_at === right.updated_at &&
      left.running_since === right.running_since &&
      left.activity === right.activity &&
      left.activity_tool === right.activity_tool &&
      left.activity_summary === right.activity_summary &&
      left.process_id === right.process_id &&
      left.delegated === right.delegated &&
      left.capabilities?.structured_events ===
        right.capabilities?.structured_events &&
      left.capabilities?.model_profile_managed ===
        right.capabilities?.model_profile_managed &&
      left.capabilities?.model_profile_active_switch ===
        right.capabilities?.model_profile_active_switch &&
      left.capabilities?.message_rewind === right.capabilities?.message_rewind
    )
  );
}

function stringArraysEqual(left: string[], right: string[]): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}

function normalizeWorker(
  agent: RawWorker,
  serverId: string,
  serverName: string,
  serverUrl: string,
): Worker {
  return {
    key: makeSessionKey(serverId, agent.id),
    id: agent.id,
    serverId,
    serverName,
    serverUrl,
    name: agent.name,
    status: agent.status,
    project: agent.project,
    cwd: agent.cwd,
    command: agent.command,
    executor_id: typeof agent.executor_id === 'string' && agent.executor_id ? agent.executor_id : undefined,
    provider: typeof agent.provider === 'string' && agent.provider ? agent.provider : undefined,
    approvals: normalizeApprovals(agent.approvals),
    prompt: normalizePrompt(agent.prompt),
    summary: agent.summary || '',
    phase: typeof agent.phase === 'string' ? agent.phase : undefined,
    attention: typeof agent.attention === 'string' ? agent.attention : undefined,
    task_class: typeof agent.task_class === 'string' ? agent.task_class : undefined,
    event_kind: typeof agent.event_kind === 'string' ? agent.event_kind : undefined,
    details_json: typeof agent.details_json === 'string' ? agent.details_json : undefined,
    // Whether it waits on the person. A delegated Worker's own report (`--attention user_input`, done, blocked) is
    // addressed to Brain, which decides and asks the person only through its Work's question (owner 2026-10-26,
    // 11-13): it raises no card, tray row, count or dot here. A prompt or an approval on its terminal still does.
    needs_attention: agent.needs_attention === true && agent.delegated !== true,
    last_output_lines: Array.isArray(agent.last_output_lines) ? agent.last_output_lines : [],
    started_at: normalizeTimestamp(agent.started_at),
    updated_at: normalizeTimestamp(agent.updated_at),
    running_since: agent.status === 'running' ? normalizeTimestamp(agent.running_since) : undefined,
    activity: agent.status === 'running' && typeof agent.activity === 'string' && agent.activity.trim() ? agent.activity.trim() : undefined,
    activity_tool: agent.status === 'running' && typeof agent.activity_tool === 'string' && agent.activity_tool.trim() ? agent.activity_tool.trim() : undefined,
    activity_summary: agent.status === 'running' && typeof agent.activity_summary === 'string' && agent.activity_summary.trim() ? agent.activity_summary.trim() : undefined,
    process_id: typeof agent.process_id === 'number' && Number.isFinite(agent.process_id)
      ? agent.process_id
      : undefined,
    delegated: agent.delegated === true,
    capabilities: normalizeWorkerCapabilities(agent.capabilities),
  };
}

export function normalizeApprovals(raw: unknown): WorkerApproval[] | undefined {
  if (!Array.isArray(raw)) {
    return undefined;
  }
  const approvals: WorkerApproval[] = [];
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue;
    const entry = item as Record<string, unknown>;
    if (typeof entry.id !== 'string' || !entry.id || !Array.isArray(entry.options)) continue;
    const options = entry.options.flatMap((option) => {
      const o = option as Record<string, unknown> | null;
      return o && typeof o.id === 'string' && o.id && typeof o.label === 'string'
        ? [{ id: o.id, label: o.label, scope: typeof o.scope === 'string' ? o.scope : undefined }]
        : [];
    });
    if (options.length === 0) continue;
    approvals.push({
      id: entry.id,
      kind: typeof entry.kind === 'string' ? entry.kind : undefined,
      tool: typeof entry.tool === 'string' ? entry.tool : undefined,
      summary: typeof entry.summary === 'string' ? entry.summary : undefined,
      options,
    });
  }
  return approvals.length > 0 ? approvals : undefined;
}

const PROMPT_KINDS: readonly WorkerPromptKind[] = ['permission', 'plan', 'choice', 'startup', 'approval'];

function optionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value ? value : undefined;
}

function normalizePromptOptions(raw: unknown): WorkerPromptOption[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const options: WorkerPromptOption[] = [];
  for (const item of raw) {
    const o = item as Record<string, unknown> | null;
    if (!o || typeof o !== 'object' || !Number.isInteger(o.number) || typeof o.label !== 'string') continue;
    options.push({
      number: o.number as number,
      label: o.label,
      description: optionalString(o.description),
      selected: o.selected === true ? true : undefined,
    });
  }
  return options;
}

/**
 * The daemon's structured prompt, or undefined when it is not one the app can
 * answer: a menu kind needs options and a choice needs questions.
 */
export function normalizePrompt(raw: unknown): WorkerPrompt | undefined {
  if (!raw || typeof raw !== 'object') {
    return undefined;
  }
  const entry = raw as Record<string, unknown>;
  const kind = PROMPT_KINDS.find((candidate) => candidate === entry.kind);
  if (typeof entry.id !== 'string' || !entry.id || !kind) {
    return undefined;
  }
  const prompt: WorkerPrompt = {
    id: entry.id,
    kind,
    provider: typeof entry.provider === 'string' ? entry.provider : '',
    title: typeof entry.title === 'string' ? entry.title : '',
    request: optionalString(entry.request),
    call_id: optionalString(entry.call_id),
  };
  const tool = optionalString(entry.tool)?.trim();
  const summary = optionalString(entry.summary)?.trim();
  if (tool) prompt.tool = tool;
  if (summary) prompt.summary = summary;
  if (kind === 'choice') {
    const questions: WorkerPromptQuestion[] = [];
    for (const item of Array.isArray(entry.questions) ? entry.questions : []) {
      const q = item as Record<string, unknown> | null;
      if (!q || typeof q !== 'object') continue;
      questions.push({
        header: optionalString(q.header),
        question: typeof q.question === 'string' ? q.question : '',
        multi_select: q.multi_select === true ? true : undefined,
        allows_other: q.allows_other === true ? true : undefined,
        answered: q.answered === true ? true : undefined,
        options: normalizePromptOptions(q.options),
      });
    }
    if (questions.length === 0) {
      return undefined;
    }
    prompt.questions = questions;
    return prompt;
  }
  const options = normalizePromptOptions(entry.options);
  if (options.length === 0) {
    return undefined;
  }
  prompt.options = options;
  return prompt;
}

function promptsEqual(left?: WorkerPrompt, right?: WorkerPrompt): boolean {
  return left === right || JSON.stringify(left ?? null) === JSON.stringify(right ?? null);
}

function approvalsEqual(left?: WorkerApproval[], right?: WorkerApproval[]): boolean {
  return JSON.stringify(left ?? null) === JSON.stringify(right ?? null);
}

function normalizeWorkerCapabilities(
  capabilities: RawWorker['capabilities'],
): WorkerCapabilities | undefined {
  return normalizeWorkerSessionCapabilities(capabilities);
}

/**
 * Timestamps are seconds or milliseconds since the Unix epoch. Invalid or
 * missing values become undefined: rows must never silently fall back to the
 * current device time, which would collapse unrelated sessions to "now" for
 * both display and ordering.
 */
function normalizeTimestamp(
  value: RawWorker['updated_at'],
): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return value > 10_000_000_000 ? value : value * 1000;
  }

  if (typeof value === 'string') {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed) && parsed > 0) return parsed;
  }

  if (value instanceof Date) {
    const parsed = value.getTime();
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }

  return undefined;
}
