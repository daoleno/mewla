import { buildAuthorizationHeader } from "./auth";
import { directPathBecameAvailable, invalidateStoredServerTransport, resolveStoredServerURL, transportFailed } from "./link/pinnedTransport";
import { platform, type SocketLike } from "./platform";
import { frameSubprotocols, frameText } from "./frames";
import type { StoredServer } from "./storedServerContract";
import { authSubprotocol } from "./webSocketAuth";

const OPEN = 1;

export type ConnectionState = "idle" | "connecting" | "connected" | "waiting";

export interface Frame {
  type: string;
  request_id?: string;
  [key: string]: unknown;
}

export interface RequestOptions {
  /** The frame type that answers the request (`stats` for `get_stats`). */
  reply: string;
  /** Frame types that fail it; `error` by default. */
  errors?: string[];
  timeoutMs?: number;
  signal?: AbortSignal;
}

export const transportTiming = {
  /** A connected socket that has carried no frame for this long is treated as dead. */
  silentLimitMs: 25_000,
  checkMs: 5_000,
  /**
   * How long the computer keeps a socket whose phone answers nothing (the daemon's wsReadIdleTimeout). An app that
   * was away (frozen in the background) and comes back to a socket this silent is on one the computer already
   * dropped: it connects again at once.
   */
  peerGivesUpMs: 60_000,
  /** An attempt (the tunnel, the sign-in, the socket's handshake) that has not opened in this long is made again. */
  connectLimitMs: 30_000,
  firstRetryMs: 1_000,
  maxRetryMs: 30_000,
  /** On Mewla Relay's relay, how often to look for a direct path (a phone that joined the computer's Wi-Fi). */
  directCheckMs: 20_000,
  /**
   * The slowest link a big message is allowed for: the computer announces a message of 64 KB or more on the
   * wire (`frame_coming`, its size), and the socket may stay quiet for as long as that size takes at this rate,
   * beyond the silent limit. Nothing of a message reaches JS until all of it is in.
   */
  slowLinkBytesPerSecond: 8_000,
  /** The most a single announced message is allowed. */
  maxComingMs: 10 * 60_000,
};

/**
 * How a turn's frames reach the listeners. The host hands JS the frames that came in together
 * (9 to 36 in a row in one call, measured), and every frame's writes rerun what reads them: a
 * burst of 36 Session updates reran the tray 36 times in one call. The app installs its UI's
 * `batch`; the frames of a turn are then handed over together on the next tick, inside one batch,
 * and whatever reads them reruns once. Without one (core's tests), each frame is handed over at once.
 */
export type FrameBatcher = (deliver: () => void) => void;
let frameBatcher: FrameBatcher | null = null;

export function setFrameBatcher(batcher: FrameBatcher | null): void {
  frameBatcher = batcher;
}

/** Resolves the socket URL for a server: the stored address, or a local tunnel the host opens (Mewla Relay). */
export type URLResolver = (server: StoredServer) => Promise<string>;

type Listener = (frame: Frame) => void;

export interface ListenOptions {
  /**
   * Heard while the app is away too (`Transport.setAway`): what has to work with the app out of sight (a notification
   * chip's answer, the live notification and widgets, a request settling) and costs little. Every other listener
   * hears the app's frames once it is back in front.
   */
  whileAway?: boolean;
}

/** One frame waiting in the inbox: parsed, or (held while away, unparsed) its text; `heard` once the away listeners had it. */
interface Waiting {
  frame?: Frame;
  text?: string;
  bytes: number;
  heard?: boolean;
}

/**
 * The one connection to the current computer. It owns reconnecting, the
 * silent-socket watchdog and a send queue: NSH's WebSocket lets at most 8 MiB
 * (in 1,024 messages) wait to go out within a turn, so a bigger burst (a long
 * paste, pictures in a message) waits here instead of failing. A server switch makes a new Transport.
 */
export class Transport {
  state: ConnectionState = "idle";
  /** Why the last attempt failed, for the connection line; empty while healthy. */
  problem = "";
  private socket: SocketLike | null = null;
  private queue: string[] = [];
  private draining = false;
  private listeners = new Map<string, Map<Listener, boolean>>();
  private stateListeners = new Set<(state: ConnectionState) => void>();
  private wanted = false;
  private attempt = 0;
  private retryMs = transportTiming.firstRetryMs;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  /** The deadline of the attempt in flight; null when none is. */
  private attemptTimer: ReturnType<typeof setTimeout> | null = null;
  /** The state the state listeners were last told. */
  private told: ConnectionState = "idle";
  private watchdog: ReturnType<typeof setInterval> | null = null;
  private directCheck: ReturnType<typeof setInterval> | null = null;
  private lastFrameAt = 0;
  /** Until when an announced message may keep the socket quiet (`frame_coming`); 0 with none on its way. */
  private comingUntil = 0;
  private nextId = 0;
  /** The frames of this turn, handed over together on the next tick (see FrameBatcher), each with its size. */
  private inbox: Waiting[] = [];
  private inboxTimer: ReturnType<typeof setTimeout> | null = null;
  /** The app is out of sight: frames wait in the inbox (`setAway`). */
  private away = false;
  /** What the frames waiting while away weigh. */
  private heldBytes = 0;
  /** Away, the frames outgrew [AWAY_HOLD_BYTES] and were let go: back in front, the app connects again. */
  private behind = false;
  /** The request ids this phone sent that wait for their answer, and when: an answer is handled at once, also while away. */
  private asked = new Map<string, number>();

  constructor(
    readonly server: StoredServer,
    private readonly resolveURL: URLResolver = resolveStoredServerURL,
  ) {}

  connect(): void {
    if (this.wanted) return;
    this.wanted = true;
    void this.open();
  }

  /** Stops for good: no reconnect, pending requests fail. */
  close(): void {
    this.wanted = false;
    this.attempt++;
    this.clearTimers();
    const socket = this.socket;
    this.socket = null;
    this.queue = [];
    // A computer that is no longer current: what it sent and was not handed over yet is dropped.
    if (this.inboxTimer) clearTimeout(this.inboxTimer);
    this.inboxTimer = null;
    this.inbox = [];
    socket?.close(1000, "closed");
    // A computer that is no longer current keeps no tunnels open; coming back measures afresh.
    void invalidateStoredServerTransport(this.server);
    this.setState("idle");
    this.emit({ type: "disconnected", intentional: true });
  }

  /**
   * Back in the foreground or on a new network: whatever cannot be working is made again now, without the backoff.
   * A socket silent past what the computer keeps is one it already dropped; a wait, or a state nothing backs, starts
   * over. Only a live socket and an attempt still within its time are kept.
   */
  resume(): void {
    if (!this.wanted) return;
    if (this.socket && this.state === "connected") {
      const why = this.silence(Date.now(), true);
      if (!why) return;
      this.kill(why);
      return this.restart();
    }
    if (this.state === "waiting" || this.stranded()) this.restart();
  }

  /** Replaces a connection that stopped answering: closes the socket as lost, or starts over, and reconnects without waiting. */
  drop(reason: string): void {
    if (!this.wanted) return;
    this.retryMs = transportTiming.firstRetryMs;
    if (this.socket) return this.kill(reason);
    if (this.stranded()) this.restart();
  }

  /**
   * Whether the state is one nothing backs: "connected" with no socket, "connecting" with no attempt in flight,
   * "waiting" with no retry to come. Only a call cut off halfway (NSH stops a call into JS past 2 s, and no catch or
   * finally runs) can leave one; nothing would ever move it on (owner 11-15: no socket for 9 min after coming back).
   */
  private stranded(): boolean {
    if (this.state === "connected") return !this.socket;
    if (this.state === "connecting") return !this.attemptTimer;
    if (this.state === "waiting") return !this.retryTimer;
    return false;
  }

  /** Starts a new attempt now: the one in flight, if any, is given up (what it brings later is dropped). */
  private restart(): void {
    this.retryMs = transportTiming.firstRetryMs;
    this.clearTimers();
    const socket = this.socket;
    this.socket = null;
    // Its attempt is over: its end changes nothing.
    this.attempt++;
    socket?.close(4000, "restart");
    void this.open();
  }

  on(type: string, listener: Listener, options: ListenOptions = {}): () => void {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Map()));
    set.set(listener, options.whileAway === true);
    return () => set.delete(listener);
  }

  /**
   * The app went out of sight (`true`) or came back in front. While away the app's JS may be frozen by the system at
   * any moment, and a call it was in the middle of is cut off on thaw (NSH counts the frozen time against its 2 s):
   * the less runs then, the fewer such cut-offs. So frames wait in the inbox, a big one unparsed, and only what has
   * to work out of sight runs at once: the answer to a request this phone sent (its first frame, to everyone who
   * listens for it) and the listeners that asked for it (`whileAway`). Back in front, every other listener hears the
   * frames in the order they came, a batch a call as always. Past [AWAY_HOLD_BYTES] (some 8 min of a busy computer's
   * news) the frames are let go instead, and back in front the app connects again for the computer's state as it is.
   */
  setAway(away: boolean): void {
    if (this.away === away) return;
    this.away = away;
    if (away) return;
    this.heldBytes = 0;
    if (this.behind) {
      this.behind = false;
      return this.drop("caught up");
    }
    if (frameBatcher) return this.scheduleInbox();
    for (const waiting of this.inbox.splice(0)) this.handOver(waiting);
  }

  onState(listener: (state: ConnectionState) => void): () => void {
    this.stateListeners.add(listener);
    return () => this.stateListeners.delete(listener);
  }

  /** Sends now or after the frames already waiting; throws only when not connected. */
  send(frame: Frame): void {
    if (this.state !== "connected" || !this.socket) throw new Error("Daemon is not connected.");
    if (typeof frame.request_id === "string") this.noteAsked(frame.request_id);
    this.queue.push(JSON.stringify(frame));
    this.drain();
  }

  /** Sends `{type, request_id, ...body}` and settles on the correlated reply, an error, the timeout, a disconnect or an abort. */
  request<T = Frame>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T> {
    const requestId = `r${Date.now().toString(36)}${(this.nextId++).toString(36)}`;
    return new Promise<T>((resolve, reject) => {
      const stops: Array<() => void> = [];
      const finish = (action: () => void) => {
        stops.forEach((stop) => stop());
        clearTimeout(timer);
        action();
      };
      const mine = (frame: Frame) => frame.request_id === requestId;
      stops.push(this.on(options.reply, (frame) => mine(frame) && finish(() => resolve(frame as T))));
      for (const errorType of options.errors ?? ["error"]) {
        stops.push(this.on(errorType, (frame) => mine(frame) && finish(() => reject(requestError(frame, `${type} failed`)))));
      }
      stops.push(this.on("disconnected", () => finish(() => reject(new Error("Daemon is not connected."))), { whileAway: true }));
      const timer = setTimeout(() => finish(() => reject(new Error(`${type} timed out`))), options.timeoutMs ?? 15_000);
      if (options.signal) {
        const abort = () => finish(() => reject(new Error(`${type} cancelled`)));
        if (options.signal.aborted) return abort();
        options.signal.addEventListener("abort", abort, { once: true });
        stops.push(() => options.signal?.removeEventListener("abort", abort));
      }
      try {
        this.send({ type, request_id: requestId, ...body });
      } catch (error) {
        finish(() => reject(error));
      }
    });
  }

  private drain(): void {
    if (this.draining) return;
    const socket = this.socket;
    while (socket && this.queue.length > 0) {
      try {
        socket.send(this.queue[0]);
        this.queue.shift();
      } catch (error) {
        if ((error as { code?: string })?.code !== "ERR_NSH_QUOTA_EXCEEDED") throw error;
        this.draining = true;
        setTimeout(() => {
          this.draining = false;
          if (this.socket === socket && socket.readyState === OPEN) this.drain();
        }, 1);
        return;
      }
    }
  }

  /*
   * Every step below sets the Transport's own fields and arms what comes next (the attempt's deadline, the retry)
   * before anything outside it runs: the state listeners, the frame and "disconnected" listeners, the tunnel's
   * bookkeeping. A listener that throws, or a call NSH cuts off past its 2 s (no catch or finally runs then), can cost
   * only what the listeners hear, never the reconnect. Before, an end whose listeners were cut off left the Transport
   * "connected" with no socket and nothing to come (owner 11-15: no socket to the computer for 9 min after coming back).
   */
  private async open(): Promise<void> {
    const attempt = ++this.attempt;
    if (this.attemptTimer) clearTimeout(this.attemptTimer);
    // A host call or socket event that never lands (the app was frozen under it) must not hold "connecting" for good.
    this.attemptTimer = setTimeout(() => this.giveUp(attempt), transportTiming.connectLimitMs);
    this.state = "connecting";
    let ready: Promise<[string, string]>;
    try {
      ready = Promise.all([
        buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: "mewla-connect" }),
        this.resolveURL(this.server),
      ]);
    } catch (error) {
      ready = Promise.reject(error);
    }
    this.tellState();
    let socket: SocketLike;
    try {
      const [authorization, url] = await ready;
      if (attempt !== this.attempt) return;
      socket = platform().openSocket(url, [authSubprotocol(authorization), ...frameSubprotocols()]);
    } catch (error) {
      if (attempt === this.attempt) this.failed(error instanceof Error ? error.message : String(error), true);
      return;
    }
    this.socket = socket;
    // The handshake gets a budget of its own: on Mewla Relay the tunnel's measurement and the bridged socket's own
    // dial each take up to some 20 s on a slow link that does work.
    if (this.attemptTimer) clearTimeout(this.attemptTimer);
    this.attemptTimer = setTimeout(() => this.giveUp(attempt), transportTiming.connectLimitMs);
    let ended = false;
    socket.onopen = () => {
      if (attempt !== this.attempt) return socket.close();
      this.clearAttempt();
      this.retryMs = transportTiming.firstRetryMs;
      this.problem = "";
      this.lastFrameAt = Date.now();
      this.startWatchdog();
      this.state = "connected";
      this.tellState();
      // Behind whatever of the last socket is still being handed over, in a call of its own.
      this.receive({ type: "connected" }, BIG_FRAME_BYTES);
    };
    socket.onmessage = (event) => {
      if (attempt !== this.attempt) return;
      this.lastFrameAt = Date.now();
      let frame: Frame;
      let bytes = 0;
      let received = "";
      try {
        const text = frameText(event.data);
        if (text === null) return;
        bytes = text.length;
        // Away, a big frame (a snapshot, a page) waits unread unless a request of this phone's may be answered by it.
        if (this.away && bytes >= BIG_FRAME_BYTES && !this.awaitingAnswers()) {
          this.comingUntil = 0;
          return this.hold({ text, bytes });
        }
        frame = JSON.parse(text);
        received = text;
      } catch {
        return;
      }
      if (!frame || typeof frame.type !== "string") return;
      if (frame.type === "frame_coming") {
        this.comingUntil = Date.now() + transportTiming.silentLimitMs + comingMs(frame.bytes);
        return;
      }
      this.comingUntil = 0;
      this.receive(frame, bytes, received);
    };
    const end = (why: string) => {
      if (ended) return;
      ended = true;
      if (this.socket === socket) this.socket = null;
      if (attempt !== this.attempt) return;
      const wasConnected = this.state === "connected";
      this.clearTimers();
      this.queue = [];
      this.armRetry(why);
      // Moving onto a direct path keeps the relay open as the fallback; any other end measures afresh.
      if (why !== "direct-available") void transportFailed(this.server);
      this.tellState();
      // What came in before the end is the computer's state as of then: it is handed over first, on the ticks it
      // was given, and the end after it in a call of its own. Handing it all over here, in the socket's own call,
      // was what took that call past 2 s on the owner's phone.
      if (wasConnected) this.receive({ type: "disconnected", intentional: false }, BIG_FRAME_BYTES);
    };
    socket.onerror = () => end("error");
    socket.onclose = (event) => end(event.reason || `closed (${event.code})`);
  }

  /** The attempt in flight is past its time: its socket is closed as lost, or, with none yet, the attempt is dropped. */
  private giveUp(attempt: number): void {
    this.attemptTimer = null;
    if (attempt !== this.attempt) return;
    if (this.socket) return this.kill("timed out");
    // What the attempt brings later is dropped.
    this.attempt++;
    this.failed("timed out", true);
  }

  /** An attempt that never opened failed: the retry is armed first, then the tunnel's bookkeeping and the listeners run. */
  private failed(problem: string, measureAfresh: boolean): void {
    this.clearAttempt();
    this.armRetry(problem);
    if (measureAfresh) void transportFailed(this.server);
    this.tellState();
  }

  /** Sets the next state and arms the retry; the state listeners are told afterwards (`tellState`). */
  private armRetry(problem: string): void {
    this.problem = problem;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    if (!this.wanted) {
      this.state = "idle";
      return;
    }
    this.state = "waiting";
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.open();
    }, this.retryMs);
    this.retryMs = Math.min(this.retryMs * 2, transportTiming.maxRetryMs);
  }

  /**
   * Why the open socket is to be given up now, or null. After the app was away (a late tick, the app back in front),
   * frames may still be queued behind the freeze: only a silence past what the computer keeps counts then.
   * A big message on its way (`frame_coming`) is not silence until its time at the slowest link is up.
   */
  private silence(now: number, away: boolean): string | null {
    if (now <= this.comingUntil) return null;
    const quiet = now - this.lastFrameAt;
    if (away) return quiet > transportTiming.peerGivesUpMs ? "stale" : null;
    return quiet > transportTiming.silentLimitMs ? "silent" : null;
  }

  private startWatchdog(): void {
    let lastTick = Date.now();
    this.watchdog = setInterval(() => {
      const now = Date.now();
      // A late tick means the app itself was away.
      const away = now - lastTick > transportTiming.checkMs * 3;
      lastTick = now;
      const why = this.silence(now, away);
      if (why) this.kill(why);
    }, transportTiming.checkMs);
    if (this.server.transportKind !== "link") return;
    // A phone that joined the computer's network after connecting through the relay moves onto
    // the direct path with one quick reconnect.
    let probing = false;
    this.directCheck = setInterval(() => {
      if (probing || this.state !== "connected") return;
      probing = true;
      void directPathBecameAvailable(this.server)
        .then((available) => {
          if (available && this.state === "connected") this.kill("direct-available");
        })
        .catch(() => undefined)
        .finally(() => (probing = false));
    }, transportTiming.directCheckMs);
  }

  /** Ends the current socket as lost (not intentional): what is waiting fails, and the retry follows. */
  private kill(reason: string): void {
    const socket = this.socket;
    socket?.close(4000, reason);
    socket?.onclose?.({ code: 4000, reason });
  }

  private clearAttempt(): void {
    if (this.attemptTimer) clearTimeout(this.attemptTimer);
    this.attemptTimer = null;
  }

  private clearTimers(): void {
    if (this.watchdog) clearInterval(this.watchdog);
    if (this.directCheck) clearInterval(this.directCheck);
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.clearAttempt();
    this.watchdog = null;
    this.directCheck = null;
    this.retryTimer = null;
  }

  private setState(state: ConnectionState): void {
    this.state = state;
    this.tellState();
  }

  /** Tells the state listeners the current state, once per change; one that throws does not cost the others. */
  private tellState(): void {
    const state = this.state;
    if (state === this.told) return;
    this.told = state;
    for (const listener of [...this.stateListeners]) {
      try {
        listener(state);
      } catch (error) {
        console.error(error);
      }
    }
  }

  /** `text`: what the frame came as, kept instead of the frame while it waits out of sight (less memory). */
  private receive(frame: Frame, bytes = 0, text?: string): void {
    // The first frame answering a request of this phone's is its answer; what follows under the same id (a
    // subscription's stream) is the computer's news.
    const answer = typeof frame.request_id === "string" && this.asked.delete(frame.request_id);
    if (this.away) {
      // An answer is handled now: what waits on it (a send, a notification's answer, a request) must not time out
      // out of sight. Ahead of the frames waiting: those are the computer's news, read back in front.
      if (answer) return this.emit(frame);
      this.emit(frame, "away");
      return this.hold(text ? { text, bytes, heard: true } : { frame, bytes, heard: true });
    }
    const batcher = frameBatcher;
    if (!batcher) return this.emit(frame);
    this.inbox.push({ frame, bytes });
    this.scheduleInbox();
  }

  private scheduleInbox(): void {
    const batcher = frameBatcher;
    if (this.inboxTimer || !batcher || this.inbox.length === 0 || this.away) return;
    this.inboxTimer = setTimeout(() => {
      this.inboxTimer = null;
      // Gone out of sight since this tick was armed: the frames wait for the app to be back.
      if (this.away) return;
      const batch = this.inbox.splice(0, takeBatch(this.inbox));
      // The rest on a tick of its own (a call of its own, with NSH's 2 s of its own), armed before this batch is
      // handed over: a batch NSH cuts off costs only itself.
      this.scheduleInbox();
      // One listener's failure costs neither the others nor the frames after it (`emit`).
      batcher(() => batch.forEach((waiting) => this.handOver(waiting)));
    }, 0);
  }

  /** A frame waits for the app to be back, while the waiting ones weigh less than [AWAY_HOLD_BYTES]; then all are let go. */
  private hold(waiting: Waiting): void {
    if (this.behind) return;
    this.heldBytes += waiting.bytes;
    if (this.heldBytes <= AWAY_HOLD_BYTES) return void this.inbox.push(waiting);
    this.behind = true;
    this.inbox = [];
  }

  /** A waiting frame to its listeners: read now if it waited unread; the away listeners only if they did not hear it. */
  private handOver(waiting: Waiting): void {
    let frame = waiting.frame;
    if (!frame) {
      try {
        frame = JSON.parse(waiting.text ?? "") as Frame;
      } catch {
        return;
      }
      if (!frame || typeof frame.type !== "string") return;
    }
    this.emit(frame, waiting.heard ? "rest" : "all");
  }

  /** Hands a frame to its listeners (all, or those heard while away, or the rest); one that throws does not cost the others. */
  private emit(frame: Frame, which: "all" | "away" | "rest" = "all"): void {
    this.listeners.get(frame.type)?.forEach((whileAway, listener) => {
      if ((which === "away" && !whileAway) || (which === "rest" && whileAway)) return;
      try {
        listener(frame);
      } catch (error) {
        console.error(error);
      }
    });
  }

  /** A request id this phone sent, waiting for its answer: one comes within minutes, or not at all. */
  private noteAsked(requestId: string): void {
    const now = Date.now();
    for (const [id, at] of this.asked) if (now - at > ASKED_FOR_MS) this.asked.delete(id);
    this.asked.set(requestId, now);
  }

  private awaitingAnswers(): boolean {
    const now = Date.now();
    for (const at of this.asked.values()) if (now - at <= ASKED_FOR_MS) return true;
    return false;
  }
}

/**
 * A frame this big is a batch of its own (a thread's snapshot, the Session list, the Work items): what reads it is
 * remade in the call that hands it over, and an open's frames together took one call 138 ms on a dev box, more than
 * 2 s on the owner's phone (11-14, v0.4.2: NSH cut it off, "Error: interrupted"). Small frames (a burst of Session
 * updates, a streaming reply) still go together, so what reads them reruns once.
 */
export const BIG_FRAME_BYTES = 32 * 1024;

/**
 * The most the frames waiting while the app is away may weigh (`Transport.setAway`): a busy computer sends some 16 KB a
 * second (the owner's, 11-15), so some 8 min of its news; past that, coming back connects again instead.
 */
export const AWAY_HOLD_BYTES = 8 * 1024 * 1024;

/**
 * The most the small frames handed over in one call may weigh: a burst (36 Session updates) is one call, as before;
 * the frames that waited while the app was away are many calls, each well inside NSH's 2 s on a slow phone.
 */
const BATCH_BYTES = 256 * 1024;

/** How long a request's answers are looked for (`Transport.setAway`): longer than any request waits. */
const ASKED_FOR_MS = 2 * 60_000;

/** How many of the inbox's first frames go in one batch: the small ones up to the next big one (at most [BATCH_BYTES] of them), or one big one. */
function takeBatch(inbox: readonly { bytes: number }[]): number {
  if (inbox.length === 0) return 0;
  if (inbox[0]!.bytes >= BIG_FRAME_BYTES) return 1;
  let weight = 0;
  for (let index = 0; index < inbox.length; index += 1) {
    const bytes = inbox[index]!.bytes;
    if (bytes >= BIG_FRAME_BYTES || (index > 0 && weight + bytes > BATCH_BYTES)) return index;
    weight += bytes;
  }
  return inbox.length;
}

/** How long an announced message of `bytes` may take on the slowest link allowed for. */
function comingMs(bytes: unknown): number {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes <= 0) return 0;
  return Math.min(Math.ceil((bytes / transportTiming.slowLinkBytesPerSecond) * 1000), transportTiming.maxComingMs);
}

/** A daemon error reply as an Error that keeps its `code` (`answer_choice_gone`, `input_uncertain`, …). */
export function requestError(frame: Frame, fallback: string): Error & { code?: string } {
  const error: Error & { code?: string } = new Error(String(frame.message || fallback));
  if (typeof frame.code === "string") error.code = frame.code;
  return error;
}
