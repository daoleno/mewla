import type { Transport } from "./transport";
import { isConversationSyncingReason } from "./session/timelinePhase";

/** A Session as Brain lists it; also Brain's own hidden host (`host_worker`). */
export type BrainWorkerRef = {
  id: string;
  name: string;
  status: string;
  summary?: string;
  phase?: string;
  cwd?: string;
  command?: string;
  /** Adapter family of the executor, as the daemon names it. */
  provider?: string;
  started_at?: number;
  process_id?: number;
  updated_at?: string;
  delegated?: boolean;
  /** Daemon-authoritative capabilities; never inferred from the name or command. */
  capabilities?: Record<string, unknown>;
  /** The blocking prompt open in the host's terminal, when it is blocked on one. */
  prompt?: unknown;
};

export type BrainExecutorRef = {
  id: string;
  name: string;
  provider?: string;
  command?: string;
  capabilities?: { interactive_tty?: boolean; structured_events?: boolean };
  host?: boolean;
};

export type BrainExecutorState = { installed: boolean; signed_in: boolean; auth_state?: "authenticated" | "unauthenticated" | "unknown"; account?: string };

/** Why Brain has no running host, said by the computer (daemon brain.HostProblem). */
export type BrainHostProblem = {
  /** not_installed, not_signed_in, not_started or not_ready; a newer computer may say more. */
  code: string;
  executor: string;
  detail: string;
  suggest_executor?: string;
  installed_executors: string[];
};

/** An agent family whose model provider is refusing requests now (daemon watcher.ProviderOutage). */
export type BrainProviderOutage = {
  /** claude, codex, pi, opencode, … */
  provider: string;
  /** RFC 3339: when the first refused request of this outage settled. */
  since: string;
  /** The newest refusal in the provider's own words (for the log, not the line). */
  cause?: string;
  /** rate limit, overloaded, server error, timeout or connection lost. */
  kind?: string;
};

const PROVIDER_NAMES: Record<string, string> = { claude: "Claude", codex: "Codex", pi: "Pi", opencode: "OpenCode", "cursor-agent": "Cursor", grok: "Grok", antigravity: "Antigravity" };

function providerName(provider: string): string {
  return PROVIDER_NAMES[provider] ?? (provider ? provider[0]!.toUpperCase() + provider.slice(1) : "The model provider");
}

function clock(at: Date, now: Date): string {
  const time = `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;
  if (now.getTime() - at.getTime() < 20 * 60 * 60 * 1000) return time;
  return `${at.toLocaleString("en-US", { month: "short" })} ${at.getDate()} ${time}`;
}

/**
 * The one line Brain's Chat shows while a model provider refuses requests, null otherwise: what is
 * unavailable, since when, and that work is held, not lost. "Claude hasn't answered since 07:03
 * (rate limit). Work is held, not lost." It clears by itself when the computer's snapshot no
 * longer lists an outage.
 */
export function providerOutageLine(snapshot: BrainSnapshot | null | undefined, now: Date = new Date()): string | null {
  const outages = (snapshot?.provider_outages ?? []).filter((outage) => outage.provider || outage.since);
  if (outages.length === 0) return null;
  const sorted = [...outages].sort((a, b) => Date.parse(a.since) - Date.parse(b.since));
  const names = [...new Set(sorted.map((outage) => providerName(outage.provider)))];
  const who = names.length === 1 ? names[0] : `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
  const since = new Date(sorted[0]!.since);
  const when = Number.isNaN(since.getTime()) ? "" : ` since ${clock(since, now)}`;
  const kind = sorted[0]!.kind ? ` (${sorted[0]!.kind})` : "";
  return `${who} ${names.length === 1 ? "hasn't" : "haven't"} answered${when}${kind}. Work is held, not lost.`;
}

/** An agent the computer installs for Brain while it has none (daemon brain.AgentInstall): `command` runs as a terminal
 * Session there (the computer's own `mewla deps -agent`), which installs it, signs in and moves Brain to it. */
export type BrainAgentInstall = { id: string; name: string; command: string };

/**
 * `brain_snapshot.brain`: Brain's state, not its conversation. The conversation
 * is the host's, followed with `codex_conversation_subscribe` in the scope of
 * `chat_thread_id`. Work (current_work, open_work, …) is read by the Work screens.
 */
export type BrainSnapshot = {
  workers?: BrainWorkerRef[];
  host_worker?: BrainWorkerRef | null;
  host_executor?: BrainExecutorRef | null;
  host_problem?: BrainHostProblem;
  executors?: BrainExecutorRef[];
  executor_states?: Record<string, BrainExecutorState>;
  agent_installs?: BrainAgentInstall[];
  /** Agent families whose provider refuses requests now; Brain's Chat shows providerOutageLine. */
  provider_outages?: BrainProviderOutage[];
  chat_thread_id?: string;
  current_work?: unknown[];
  open_work?: unknown[];
  workspace?: string;
  generated_at?: string;
};

/** The conversation scope of a Brain chat thread; drafts and the subscription use it. */
export function brainThreadScopeKey(threadId: string): string {
  return `brain-thread:${threadId}`;
}

/** Whether a chat's cache key (`<server>:scope:<scope>`) is that of this Brain thread. */
export function isBrainThreadCacheKey(cacheKey: string, threadId: string): boolean {
  return threadId !== "" && cacheKey.endsWith(`:scope:${brainThreadScopeKey(threadId)}`);
}

/** Asks for Brain's state; the answer arrives as a `brain_snapshot` like every broadcast. Never throws. */
export function requestBrainSnapshot(transport: Transport): void {
  try {
    transport.send({ type: "brain_snapshot" });
  } catch {
    // Not connected: the daemon sends one on the next connection.
  }
}

/** Starts a new Brain chat; settles with the snapshot that carries the new thread. */
export function newBrainChat(transport: Pick<Transport, "request">): Promise<BrainSnapshot> {
  return transport
    .request<{ brain?: BrainSnapshot }>("brain_chat_new", {}, { reply: "brain_snapshot" })
    .then((frame) => frame.brain ?? {});
}

/** The agents to install while Brain can't start because no agent is on the computer ("No agent on this Mac"); empty
 * otherwise: an installed agent that is signed out is a sign-in, not an install. */
export function brainAgentInstalls(snapshot: BrainSnapshot | null | undefined): BrainAgentInstall[] {
  if (snapshot?.host_problem?.code !== "not_installed") return [];
  if (Object.values(snapshot.executor_states ?? {}).some((state) => state.installed)) return [];
  return (snapshot.agent_installs ?? []).filter((install) => install.id && install.command);
}

/**
 * What Brain's screen shows. `waking` until the computer reports a running host
 * (the composer keeps the draft meanwhile); `host_down` when the computer says
 * why there is none, which waiting will not fix.
 */
export type BrainSurface = "no_computer" | "offline" | "waking" | "host_down" | "chat";

export function brainSurface(input: { hasServer: boolean; connected: boolean; snapshot: BrainSnapshot | null }): BrainSurface {
  if (!input.hasServer) return "no_computer";
  const { snapshot } = input;
  // The computer states a problem only while the host is not running, so it outranks a recorded host id.
  if (snapshot?.host_problem) return input.connected ? "host_down" : "offline";
  if (snapshot?.host_worker?.id && snapshot.chat_thread_id) return "chat";
  return input.connected ? "waking" : "offline";
}

/**
 * Brain runs on an agent without structured chat events: its chat cannot be read,
 * only its terminal ("Chat isn't available for {agent}", soft-final/SPEC.md §1.3).
 * The computer always states `structured_events`; a snapshot without it reads as a chat.
 */
export function brainChatUnavailable(snapshot: BrainSnapshot | null | undefined): boolean {
  return snapshot?.host_executor?.capabilities?.structured_events === false;
}

/**
 * What Brain's chat area shows when it has no rows (soft-final/SPEC.md §1.3):
 * "loading", "failed" ("Couldn't load the chat": a load error, or a transcript the
 * computer cannot read), or "empty". A transcript still syncing reads as loading.
 */
export type BrainChatState = "content" | "loading" | "failed" | "empty";

export function brainChatState(input: {
  itemCount: number;
  loading: boolean;
  error?: string | null;
  conversation?: { available?: boolean; reason?: string } | null;
}): BrainChatState {
  if (input.itemCount > 0) return "content";
  if (input.loading) return "loading";
  if (input.error) return "failed";
  const conversation = input.conversation;
  if (conversation && conversation.available === false) {
    // No transcript yet is a chat with nothing in it (a fresh thread), as a Session reads it.
    if (conversation.reason === "transcript_not_found") return "empty";
    return isConversationSyncingReason(conversation.reason) ? "loading" : "failed";
  }
  return "empty";
}

/**
 * Why Brain's composer cannot send now, for its placeholder (soft-final/SPEC.md
 * §1.3); null when it can. Offline is not a reason: sends queue.
 */
export function brainComposerBlock(input: { surface: BrainSurface; snapshot: BrainSnapshot | null | undefined; chat: BrainChatState }): string | null {
  if (input.surface === "host_down") return "Brain can't start";
  if (input.surface !== "chat") return null;
  if (brainChatUnavailable(input.snapshot)) return "Chat isn't available";
  return input.chat === "failed" ? "Chat didn't load" : null;
}
