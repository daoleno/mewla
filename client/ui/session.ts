import { signal, Storage } from "nsh";
import { brainThreadScopeKey, requestBrainSnapshot, type BrainSnapshot } from "../core/brain";
import { Chat } from "../core/conversation/chat";
import { chatCacheKey, Draft } from "../core/conversation/draft";
import { createOutbox } from "../core/conversation/outboxRuntime";
import { forgetInterfaceConversationsOutsideServer, initialInterfaceChatThreadState, type InterfaceChatThreadState } from "../core/conversation/thread";
import { getCurrentServerId, getServers, markOnboarded, saveServer, setCurrentServerId, type StoredServer } from "../core/servers";
import { WorkerSessions } from "../core/sessions/store";
import type { Worker } from "../core/sessions/workers";
import { Transport, type ConnectionState } from "../core/transport";

/**
 * Everything that belongs to the current computer. There is exactly one; switching
 * computers closes this one's transport and Brain chat and starts fresh, so nothing of
 * the previous computer's state survives into the next.
 */
export const servers = signal<StoredServer[]>([]);
export const current = signal<StoredServer | null>(null);
export const connection = signal<ConnectionState>("idle");
export const problem = signal("");
export const ready = signal(false);
/** `brain_snapshot.brain` of the current computer; null until the first one. */
export const brain = signal<BrainSnapshot | null>(null);
/** Brain's open chat (its current thread on its current host), and what it shows. */
export const brainChat = signal<Chat | null>(null);
export const brainThread = signal<InterfaceChatThreadState>(initialInterfaceChatThreadState(""));
/** The draft of Brain's current thread: there as soon as the thread is known, whether or not Brain's host runs. */
export const brainDraft = signal<Draft | null>(null);
/**
 * A past Brain thread a link opened (a Brain notification names its thread):
 * Brain's chat shows it read-only until it is set back to null. Null shows
 * Brain's current thread; a link to the current thread leaves it null.
 */
export const routedBrainThread = signal<string | null>(null);

let transport: Transport | null = null;
let stops: Array<() => void> = [];
let wakeTimer: ReturnType<typeof setInterval> | null = null;

/** The app's one send queue; a server's messages go only over that server's live connection. */
export const outbox = createOutbox(Storage, (serverId) => (transport && current()?.id === serverId ? transport : null));
void outbox.hydrate();

/** While Brain's host is starting, ask again now and then: a missed broadcast must never leave Brain "starting". */
const WAKE_POLL_MS = 3_000;

export function currentTransport(): Transport | null {
  return transport;
}

type TransportUser = (transport: Transport, server: StoredServer) => (() => void) | void;
const transportUsers = new Set<TransportUser>();
let transportCleanups: Array<() => void> = [];

/**
 * How a feature follows the current computer: `use` runs with each computer's
 * Transport (now, if there is one, and after every switch); what it returns runs
 * when that computer stops being current, so the feature drops its state then.
 * Listen with `transport.on(...)` inside; the Transport reconnects by itself.
 */
export function onTransport(use: TransportUser): () => void {
  transportUsers.add(use);
  const server = current();
  if (transport && server) {
    const cleanup = use(transport, server);
    if (cleanup) transportCleanups.push(cleanup);
  }
  return () => transportUsers.delete(use);
}

/**
 * The current computer's Sessions, for every screen that lists or opens one
 * (Sessions, Work, Calendar, Brain's Work cards). Empty, and not `hydrated`,
 * until this connection's list arrives; cleared on a computer switch.
 */
export const workers = signal<Worker[]>([]);
export const workersHydrated = signal(false);
/** False after a create whose outcome is unknown, until a fresh list shows what happened. */
export const canCreateSession = signal(true);
let workerSessions: WorkerSessions | null = null;

export function currentWorkerSessions(): WorkerSessions | null {
  return workerSessions;
}

onTransport((owner, server) => {
  const store = new WorkerSessions(owner, server, (next) => {
    if (workerSessions !== next) return;
    workers.set(next.workers);
    workersHydrated.set(next.hydrated);
    canCreateSession.set(next.canCreate);
  });
  workerSessions = store;
  return () => {
    store.stop();
    if (workerSessions === store) workerSessions = null;
    workers.set([]);
    workersHydrated.set(false);
    canCreateSession.set(true);
  };
});

export async function loadSession(): Promise<void> {
  const list = await getServers();
  servers.set(list);
  const id = await getCurrentServerId();
  await switchTo(list.find((s) => s.id === id) ?? list[0] ?? null);
  ready.set(true);
}

/**
 * NSH 0.4.0 crashes when the page under an open sheet is torn down, as a switch of
 * computer does. So a switch first closes every sheet (`switching`, which AppSheet
 * and GuardedSheet follow), lets them slide away, then switches. With no sheet open
 * it switches at once.
 */
export const switching = signal(false);
const SHEET_CLOSE_MS = 400;
let openSheets = 0;
let switchTarget: StoredServer | null = null;
let switchDone: Promise<void> | null = null;

/** A sheet opened; what it returns says it closed. */
export function sheetOpened(): () => void {
  openSheets += 1;
  let closed = false;
  return () => {
    if (closed) return;
    closed = true;
    openSheets -= 1;
  };
}

/** Makes `server` the current computer (null: none). Resolves once it is. */
export function switchTo(server: StoredServer | null): Promise<void> {
  switchTarget = server;
  if (switchDone) return switchDone;
  if (openSheets === 0) {
    applySwitch(server);
    return Promise.resolve();
  }
  switching.set(true);
  switchDone = new Promise<void>((resolve) => {
    setTimeout(() => {
      switchDone = null;
      switching.set(false);
      applySwitch(switchTarget);
      resolve();
    }, SHEET_CLOSE_MS);
  });
  return switchDone;
}

function applySwitch(server: StoredServer | null): void {
  const previous = current();
  if (previous) outbox.onDisconnected(previous.id);
  stops.splice(0).forEach((stop) => stop());
  transportCleanups.splice(0).forEach((cleanup) => cleanup());
  stopWakePoll();
  closeBrainChat();
  brainDraft()?.flush();
  brainDraft.set(null);
  transport?.close();
  transport = null;
  brain.set(null);
  routedBrainThread.set(null);
  current.set(server);
  connection.set("idle");
  problem.set("");
  forgetInterfaceConversationsOutsideServer(server?.id ?? "");
  if (!server) return;
  void setCurrentServerId(server.id);
  const next = new Transport(server);
  transport = next;
  for (const use of transportUsers) {
    const cleanup = use(next, server);
    if (cleanup) transportCleanups.push(cleanup);
  }
  void reopenLastBrainChat(server, next);
  stops = [
    next.onState((state) => {
      connection.set(state);
      problem.set(next.problem);
    }),
    next.on("connected", () => {
      outbox.onConnected(server.id);
      requestBrainSnapshot(next);
    }),
    next.on("disconnected", () => {
      outbox.onDisconnected(server.id);
      stopWakePoll();
    }),
    next.on("brain_snapshot", (frame) => {
      const snapshot = frame.brain as BrainSnapshot | undefined;
      if (snapshot && typeof snapshot === "object") applyBrainSnapshot(server, next, snapshot);
    }),
  ];
  next.connect();
}

/** Opens Brain's chat for the thread and host the computer reports, and keeps it in step as they change. */
function applyBrainSnapshot(server: StoredServer, owner: Transport, snapshot: BrainSnapshot): void {
  if (transport !== owner) return;
  brain.set(snapshot);
  if (snapshot.chat_thread_id) {
    const key = chatCacheKey(server.id, "", brainThreadScopeKey(snapshot.chat_thread_id));
    if (brainDraft()?.cacheKey !== key) {
      brainDraft()?.flush();
      brainDraft.set(new Draft(Storage, key));
    }
  }
  const host = snapshot.host_problem ? null : snapshot.host_worker;
  const threadId = snapshot.chat_thread_id;
  if (routedBrainThread() && routedBrainThread() === threadId) routedBrainThread.set(null);
  if (!host?.id || !threadId) {
    if (!snapshot.host_problem) startWakePoll(owner);
    return;
  }
  stopWakePoll();
  void Storage.setItem(LAST_BRAIN_CHAT + server.id, JSON.stringify({ host_worker: host, chat_thread_id: threadId })).catch(() => {});
  const scope = brainThreadScopeKey(routedBrainThread() ?? threadId);
  const open = brainChat();
  if (open && open.cacheKey.endsWith(`:scope:${scope}`) && openHost === host.id) return;
  closeBrainChat();
  // A restarted host answers the same thread under a new id: what waited for the old one goes to it.
  outbox.retarget(server.id, scope, host.id);
  const chat = new Chat({
    serverId: server.id,
    transport: owner,
    outbox,
    target: {
      targetId: host.id,
      workerId: host.id,
      cwd: host.cwd,
      command: host.command,
      name: host.name,
      startedAt: host.started_at,
      processId: host.process_id,
      conversationScopeKey: scope,
    },
  });
  openHost = host.id;
  brainThread.set(chat.current);
  stopChat = chat.onChange((state) => brainThread.set(state));
  brainChat.set(chat);
}

/**
 * Brain's last thread and host on each computer: a start without a connection opens
 * that chat at once (its draft and queued messages, the transcript once connected)
 * instead of a dead "offline" screen.
 */
const LAST_BRAIN_CHAT = "mewla:v1:brain_last_chat:";

async function reopenLastBrainChat(server: StoredServer, owner: Transport): Promise<void> {
  try {
    const raw = await Storage.getItem(LAST_BRAIN_CHAT + server.id);
    const last = raw ? (JSON.parse(raw) as BrainSnapshot) : null;
    if (!last?.host_worker?.id || !last.chat_thread_id || transport !== owner || brain()) return;
    applyBrainSnapshot(server, owner, last);
  } catch {
    // Nothing remembered: the first snapshot opens the chat.
  }
}

/**
 * Shows one of Brain's threads on the current computer: a past one read-only,
 * the current one (or none) as Brain's live chat.
 */
export function openBrainThread(threadId: string | null): void {
  const live = brain()?.chat_thread_id;
  routedBrainThread.set(threadId && threadId !== live ? threadId : null);
  const server = current();
  const snapshot = brain();
  if (transport && server && snapshot) applyBrainSnapshot(server, transport, snapshot);
}

let openHost = "";
let stopChat: (() => void) | null = null;

function closeBrainChat(): void {
  stopChat?.();
  stopChat = null;
  brainChat()?.close();
  brainChat.set(null);
  openHost = "";
  brainThread.set(initialInterfaceChatThreadState(""));
}

function startWakePoll(owner: Transport): void {
  if (wakeTimer) return;
  wakeTimer = setInterval(() => {
    if (transport === owner && owner.state === "connected") requestBrainSnapshot(owner);
  }, WAKE_POLL_MS);
}

function stopWakePoll(): void {
  if (wakeTimer) clearInterval(wakeTimer);
  wakeTimer = null;
}

export async function addPairedServer(server: Parameters<typeof saveServer>[0]): Promise<void> {
  const saved = await saveServer(server);
  await markOnboarded();
  servers.set(await getServers());
  await switchTo(saved);
}

export function resumeConnection(): void {
  transport?.resume();
}
