import { buildAuthorizationHeader } from "./auth";
import { bindingFor, CONNECT_PROOF_PURPOSE, daemonProves, newChallenge, noteDaemonProves, olderMewlaLine, proofAnswers, runsOlderMewla } from "./daemonProof";
import {
  checkDirectPath,
  invalidateStoredServerTransport,
  moveAbandoned,
  resolveStoredServerURL,
  transportFailed,
  transportMoved,
  transportOpened,
  adoptRelayConnection,
  transportPathInfo,
} from "./link/pinnedTransport";
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
  /**
   * After the phone's network changed under a live socket (`networkChanged`), how long the socket has to carry
   * something before it is taken for one left on the network that went away.
   */
  networkProofMs: 5_000,
  /**
   * On the relay, how often a direct path (the computer's LAN or Tailscale address) is checked for the connection to
   * move onto (`Transport`'s make-before-break), and the longest the wait between checks grows to while none answers.
   */
  moveCheckMs: 15_000,
  moveCheckMaxMs: 5 * 60_000,
  /** How many checks in a row the direct path has to pass before the connection moves onto it (some 30 s healthy). */
  moveHealthyChecks: 3,
  /** The new socket of a move has this long to open; it is given up otherwise, the relay's left as it was. */
  moveOpenLimitMs: 10_000,
  /** After a move that failed (or a direct socket that died within `moveTrustMs`), the first wait before another. */
  moveBackoffMs: 60_000,
  moveBackoffMaxMs: 10 * 60_000,
  moveTrustMs: 2 * 60_000,
  /** How long the old socket of a move is kept for the answers it still owes. */
  retireLimitMs: 30_000,
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

/**
 * What a connection on the relay needs to move onto a direct path (Mewla Relay's `pinnedTransport`): a check that a
 * direct path answers now, beside the live one, and the move's outcome.
 */
export interface PathMoves {
  /** The path the connection to the computer is on now ("relay", "lan", "tailscale"), or null when it has none. */
  current(server: StoredServer): string | null;
  check(server: StoredServer): Promise<{ url: string; path: string } | null>;
  moved(server: StoredServer): void;
  abandoned(server: StoredServer): void;
}

const linkMoves: PathMoves = {
  current: (server) => transportPathInfo(server.id)?.path ?? null,
  check: checkDirectPath,
  moved: transportMoved,
  abandoned: moveAbandoned,
};

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
  /** The check that the live socket still works on the phone's new network (`networkChanged`). */
  private networkProof: ReturnType<typeof setTimeout> | null = null;
  private lastFrameAt = 0;
  /** When the live socket opened, and on which path: what a drop report says (`reportLost`). */
  private openedAt = 0;
  private openedPath = "";
  /** The last live socket's end, until the next one opens and reports it; with what happened in between. */
  private lost: {
    why: string;
    /** The socket's own error text (NSH's `error` event message), when it ended in one. */
    error: string;
    path: string;
    ageMs: number;
    away: boolean;
    /** How long the app had been out of sight when it ended; 0 in front. */
    awayMs: number;
    at: number;
    attempts: number;
    network: number;
    /** The error text of the last attempt that failed after it, if one did. */
    lastError: string;
  } | null = null;
  /** Until when an announced message may keep the socket quiet (`frame_coming`); 0 with none on its way. */
  private comingUntil = 0;
  private nextId = 0;
  /** The frames of this turn, handed over together on the next tick (see FrameBatcher), each with its size. */
  private inbox: Waiting[] = [];
  private inboxTimer: ReturnType<typeof setTimeout> | null = null;
  /** The app is out of sight: frames wait in the inbox (`setAway`). */
  private away = false;
  private awaySince = 0;
  /** What the frames waiting while away weigh. */
  private heldBytes = 0;
  /** Away, the frames outgrew [AWAY_HOLD_BYTES] and were let go: back in front, the app connects again. */
  private behind = false;
  /** The request ids this phone sent that wait for their answer, and when: an answer is handled at once, also while away. */
  private asked = new Map<string, number>();
  /** On the relay: the next check for a direct path to move onto (`checkMove`). */
  private moveTimer: ReturnType<typeof setTimeout> | null = null;
  private moveWaitMs = transportTiming.moveCheckMs;
  private moveChecking = false;
  /** Bumped whenever a move is given up: a move still signing in finds it changed and stops. */
  private moveEpoch = 0;
  /** The direct path's checks passed in a row. */
  private healthyChecks = 0;
  /** No move is tried before this (a failed one backs off). */
  private moveBackoffUntil = 0;
  private moveBackoffMs = transportTiming.moveBackoffMs;
  /** The new socket of a move, until it opens (`swap`) or is given up (`abandonMove`). */
  private moving: { socket: SocketLike; timer: ReturnType<typeof setTimeout>; path: string } | null = null;
  /** The old socket of a move, kept for the answers it owes (`retire`). */
  private retiring: { socket: SocketLike; owed: Set<string>; timer: ReturnType<typeof setTimeout> } | null = null;
  /** The live socket came by a move (not a connect): one that dies soon after counts against moving. */
  private openedByMove = false;
  /**
   * The connect's own nonce, which the computer's first frame signs (`daemon_proof`, daemon/server handleWS). A computer
   * that has proved before is held to it (`gate`): until its proof verifies the socket is not connected, so nothing
   * is sent to it and nothing it sends is heard (security review 11-23, PR B item 6).
   */
  private proof: { attempt: number; nonceHex: string; gate: Array<{ data: unknown }> | null; passed: boolean } | null = null;
  /**
   * Why a computer held to its proof is not connected, when the cause is known: it runs a Mewla from before proofs (a
   * downgrade), which refuses this app's bound connect. Said in place of a bare "error" until a connect succeeds.
   */
  private olderMewla = "";
  private checkingOlder = false;

  constructor(
    public server: StoredServer,
    private readonly resolveURL: URLResolver = resolveStoredServerURL,
    private readonly moves: PathMoves = linkMoves,
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

  /**
   * The phone's network changed (the host's `link.network`): a network became the default one, or came back. Offline
   * nothing is done: the system ends the sockets it took away, and their ends say so. Online, a wait for the next retry
   * ends now instead of waiting out the backoff, and the backoff starts over. An attempt in flight goes on (a second
   * one now would race it for the same tunnel); if it fails, the next one follows after the first retry's wait. A live
   * socket is asked for an answer (`link_ping`): one that carries nothing within `networkProofMs` was left on the old
   * network and is replaced now, not after `silentLimitMs` of silence.
   */
  networkChanged(online: boolean): void {
    if (this.lost) this.lost.network++;
    // A new network may put the phone beside the computer: the direct path is checked afresh, from the first check.
    this.healthyChecks = 0;
    this.moveWaitMs = transportTiming.moveCheckMs;
    this.abandonMove("network changed");
    if (!this.wanted || !online) return;
    if (this.socket && this.state === "connected") {
      this.scheduleMoveCheck();
      return this.proveSocket();
    }
    if (this.state === "waiting" || this.stranded()) return this.restart();
    this.retryMs = transportTiming.firstRetryMs;
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
    // Its attempt is over: its end changes nothing, so what waited to go out on it is dropped here, as an end would
    // (round 2 F8): none of it goes out on the next socket.
    this.attempt++;
    this.queue = [];
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
    this.awaySince = away ? Date.now() : 0;
    if (away) return this.abandonMove("away");
    this.scheduleMoveCheck();
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
          // The socket may have been moved meanwhile (`swap`): what waits goes out on the current one.
          if (this.socket?.readyState === OPEN) this.drain();
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
    const nonceHex = newChallenge();
    let ready: Promise<[string, string, boolean]>;
    try {
      ready = (async () => {
        const [url, proves] = await Promise.all([this.resolveURL(this.server), daemonProves(this.server.daemonPublicKey)]);
        // Bound to where it goes once the computer signs challenges (older mewla refuses a bound header).
        const binding = proves ? await bindingFor(this.server, url) : null;
        const authorization = await buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: "mewla-connect", nonceHex, binding });
        return [authorization, url, proves];
      })();
    } catch (error) {
      ready = Promise.reject(error);
    }
    this.tellState();
    let socket: SocketLike;
    try {
      const [authorization, url, proves] = await ready;
      if (attempt !== this.attempt) return;
      this.proof = { attempt, nonceHex, gate: proves ? [] : null, passed: false };
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
    socket.onopen = () => {
      if (attempt !== this.attempt) return socket.close();
      // Held to its proof: connected only once the first frame proves it (`passGate`), within the attempt's time; once
      // only, should the proof have come first.
      if ((this.proof?.attempt === attempt && this.proof.gate) || (this.socket === socket && this.state === "connected")) return;
      this.opened();
    };
    this.listen(socket, attempt);
  }

  /** The socket is open and, where the computer is held to it, proved: the connection is up. */
  private opened(): void {
    this.olderMewla = "";
    this.clearAttempt();
    this.retryMs = transportTiming.firstRetryMs;
    const lastProblem = this.problem;
    this.problem = "";
    this.lastFrameAt = Date.now();
    this.openedAt = this.lastFrameAt;
    this.openedByMove = false;
    // The path this socket is carried on stays its own: no other caller re-measures it while it is open.
    transportOpened(this.server);
    this.openedPath = this.moves.current(this.server) ?? (this.server.transportKind === "link" ? "link" : "address");
    this.startWatchdog();
    this.state = "connected";
    this.healthyChecks = 0;
    this.moveWaitMs = transportTiming.moveCheckMs;
    this.scheduleMoveCheck();
    this.reportLost(lastProblem);
    this.tellState();
    // Behind whatever of the last socket is still being handed over, in a call of its own.
    this.receive({ type: "connected" }, BIG_FRAME_BYTES);
  }

  /**
   * The first frame of a socket held to its proof: the computer's signature over this connect's nonce, or the socket is
   * not this computer's (whoever answered at the address could not sign it) and is given up. What came behind the
   * proof meanwhile is heard once it passed, in order.
   */
  private async passGate(socket: SocketLike, attempt: number, gate: Array<{ data: unknown }>): Promise<void> {
    const proof = this.proof;
    let frame: Frame | null = null;
    try {
      const text = frameText(gate[0].data);
      frame = text === null ? null : JSON.parse(text);
    } catch {
      frame = null;
    }
    const proved =
      frame?.type === "daemon_proof" &&
      !!proof &&
      (await proofAnswers({ purpose: CONNECT_PROOF_PURPOSE, challengeHex: proof.nonceHex, daemonId: this.server.daemonId, daemonPublicKey: this.server.daemonPublicKey, proof: frame }));
    if (attempt !== this.attempt || this.socket !== socket || this.proof !== proof || !proof) return;
    if (!proved) return this.kill(`${this.server.name || "The computer"} did not prove it is the computer this app paired`);
    proof.passed = true;
    proof.gate = null;
    this.opened();
    for (const event of gate.slice(1)) socket.onmessage?.(event);
  }

  /** The live socket's frames and end. */
  private listen(socket: SocketLike, attempt: number): void {
    let ended = false;
    socket.onmessage = (event) => {
      if (attempt !== this.attempt) return;
      const gate = this.proof?.attempt === attempt ? this.proof.gate : null;
      if (gate) {
        gate.push(event);
        if (gate.length === 1) void this.passGate(socket, attempt, gate);
        return;
      }
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
      if (frame.type === "daemon_proof") return void this.noteProof(frame, attempt);
      if (frame.type === "frame_coming") {
        this.comingUntil = Date.now() + transportTiming.silentLimitMs + comingMs(frame.bytes);
        return;
      }
      this.comingUntil = 0;
      this.receive(frame, bytes, received);
    };
    const end = (why: string, error = "") => {
      if (ended) return;
      ended = true;
      if (this.socket === socket) this.socket = null;
      if (attempt !== this.attempt) return;
      const wasConnected = this.state === "connected";
      if (!wasConnected) this.heldAttemptFailed(attempt);
      if (wasConnected) {
        const now = Date.now();
        const awayMs = this.away && this.awaySince ? now - this.awaySince : 0;
        this.lost = { why, error, path: this.openedPath, ageMs: now - this.openedAt, away: this.away, awayMs, at: now, attempts: 0, network: 0, lastError: "" };
        // A direct path that did not hold for long after a move is not moved onto again soon, and less often each
        // time (no ping-pong); one that held earns moves at the first backoff again.
        if (this.openedByMove) {
          if (now - this.openedAt < transportTiming.moveTrustMs) this.backOffMoves();
          else this.moveBackoffMs = transportTiming.moveBackoffMs;
        }
      } else if (this.lost) {
        this.lost.attempts++;
        if (error) this.lost.lastError = error;
      }
      this.clearTimers();
      this.queue = [];
      this.armRetry(why);
      void transportFailed(this.server);
      this.tellState();
      // What came in before the end is the computer's state as of then: it is handed over first, on the ticks it
      // was given, and the end after it in a call of its own. Handing it all over here, in the socket's own call,
      // was what took that call past 2 s on the owner's phone.
      if (wasConnected) this.receive({ type: "disconnected", intentional: false }, BIG_FRAME_BYTES);
    };
    // NSH's error event says what ended the socket (a reset, an end without a close frame, a write that timed out):
    // kept for the drop report, the one place it is told.
    socket.onerror = (event) => end("error", errorText(event));
    socket.onclose = (event) => end(event.reason || `closed (${event.code})`);
  }

  /** A proof from a computer not yet held to one: it proves now, so it is from then on. */
  private async noteProof(frame: Frame, attempt: number): Promise<void> {
    const proof = this.proof;
    if (!proof || proof.attempt !== attempt || proof.passed) return;
    proof.passed = true;
    if (await proofAnswers({ purpose: CONNECT_PROOF_PURPOSE, challengeHex: proof.nonceHex, daemonId: this.server.daemonId, daemonPublicKey: this.server.daemonPublicKey, proof: frame })) {
      await noteDaemonProves(this.server.daemonPublicKey);
    }
  }

  /**
   * A connect to a computer held to its proof failed before it was up: once per run of failures, the computer is asked
   * (its /health, answered by its key) whether it runs a Mewla from before proofs, and if so that is what is said.
   */
  private heldAttemptFailed(attempt: number): void {
    if (this.proof?.attempt !== attempt || !this.proof.gate || this.olderMewla || this.checkingOlder) return;
    this.checkingOlder = true;
    void (async () => {
      try {
        const url = await this.resolveURL(this.server);
        if (!(await runsOlderMewla(this.server, url)) || this.state === "connected") return;
        this.olderMewla = olderMewlaLine(this.server.name);
        if (!this.wanted) return;
        this.problem = this.olderMewla;
        this.tellState();
      } catch {
        // Unreachable: the failure says what it says.
      } finally {
        this.checkingOlder = false;
      }
    })();
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
    if (this.lost) this.lost.attempts++;
    this.clearAttempt();
    this.armRetry(problem);
    if (measureAfresh) void transportFailed(this.server);
    this.tellState();
  }

  /** Sets the next state and arms the retry; the state listeners are told afterwards (`tellState`). */
  private armRetry(problem: string): void {
    this.problem = this.olderMewla || problem;
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
  }

  /**
   * After a network change under the live socket: asks the computer for an answer, and replaces the socket if nothing
   * at all came in by `networkProofMs` (a big message announced and still on its way counts as something). A check
   * that ends late, the app having been away, judges by what the computer keeps instead, as `resume` does.
   */
  private proveSocket(): void {
    if (this.networkProof) return;
    const socket = this.socket;
    const sentAt = Date.now();
    try {
      this.sendQuietly({ type: "link_ping", request_id: `ping-${sentAt.toString(36)}` });
    } catch {
      return;
    }
    this.networkProof = setTimeout(() => {
      this.networkProof = null;
      if (this.socket !== socket || this.state !== "connected") return;
      const now = Date.now();
      if (this.lastFrameAt >= sentAt || now <= this.comingUntil) return;
      const late = now - sentAt > transportTiming.networkProofMs * 3;
      const why = late ? this.silence(now, true) : "network changed";
      if (why) this.kill(why);
    }, transportTiming.networkProofMs);
  }

  /**
   * Tells the computer how the last live socket ended, once the next one is open (`client_diagnostic` `link_drop`, one
   * line in its log): why this end of it gave up (or the socket's own close), on which path, how long it had lived, how
   * long the phone was without one, and what happened meanwhile. It is what says which drops were the network's, the
   * app's or this connection's own.
   */
  private reportLost(lastProblem: string): void {
    const lost = this.lost;
    this.lost = null;
    if (!lost) return;
    const seconds = (ms: number) => `${(ms / 1000).toFixed(1)}s`;
    const word = (text: string, max = 60) => text.replace(/\s+/g, "_").slice(0, max) || "-";
    try {
      this.sendQuietly({
        type: "client_diagnostic",
        name: "link_drop",
        request_id: `drop-${lost.at.toString(36)}`,
        body: [
          `why=${word(lost.why)}`,
          `path=${lost.path}`,
          `age=${seconds(lost.ageMs)}`,
          `down=${seconds(Date.now() - lost.at)}`,
          `attempts=${lost.attempts}`,
          `last=${word(lastProblem)}`,
          `away=${lost.away}`,
          `network_changes=${lost.network}`,
          ...(lost.away ? [`away_for=${seconds(lost.awayMs)}`] : []),
          ...(lost.error ? [`error=${word(lost.error, 120)}`] : []),
          ...(lost.lastError ? [`last_error=${word(lost.lastError, 120)}`] : []),
        ].join(" "),
      });
    } catch {
      // A report is not worth the connection.
    }
  }

  /** Sends a frame no answer is waited on (`asked` stays the requests whose answers matter while away). */
  private sendQuietly(frame: Frame): void {
    if (this.state !== "connected" || !this.socket) throw new Error("Daemon is not connected.");
    this.queue.push(JSON.stringify(frame));
    this.drain();
  }

  /**
   * The computer's pairing became a Mewla Relay route under this connection (`linkRouteChange`): from now on it is
   * reached as one, and the open socket counts as on the relay, so it moves onto a direct path as any Relay connection.
   */
  adopt(server: StoredServer): void {
    if (server.id !== this.server.id || server.transportKind !== "link") return;
    this.server = server;
    if (this.state !== "connected" || !this.socket) return;
    adoptRelayConnection(server);
    this.openedPath = this.moves.current(server) ?? "relay";
    this.scheduleMoveCheck();
  }

  /** Ends the current socket as lost (not intentional): what is waiting fails, and the retry follows. */
  private kill(reason: string): void {
    const socket = this.socket;
    // The reason is told first: a platform whose close event comes at once (Bun's) would tell its own instead.
    socket?.onclose?.({ code: 4000, reason });
    socket?.close(4000, reason);
  }

  private clearAttempt(): void {
    if (this.attemptTimer) clearTimeout(this.attemptTimer);
    this.attemptTimer = null;
  }

  private clearTimers(): void {
    if (this.watchdog) clearInterval(this.watchdog);
    if (this.networkProof) clearTimeout(this.networkProof);
    if (this.retryTimer) clearTimeout(this.retryTimer);
    if (this.moveTimer) clearTimeout(this.moveTimer);
    this.clearAttempt();
    this.watchdog = null;
    this.networkProof = null;
    this.retryTimer = null;
    this.moveTimer = null;
    // The live socket is over: a move off it, and the socket it moved from, are over with it.
    this.abandonMove("ended");
    this.endRetired();
  }

  /*
   * Make-before-break. A connection on the relay checks, beside it, whether the computer answers on a direct path
   * (`moveCheckMs`, longer apart while none does). After `moveHealthyChecks` in a row it opens a second socket there,
   * signed in like any other. Once that one is open the Transport moves onto it in one step: the state stays
   * "connected", nothing is told "disconnected", and what waits to go out goes out on the new socket. The old socket
   * stays for the answers it owes (requests asked on it), then is closed with a normal close. The computer sends its
   * snapshots to the new socket as to any; what it keeps per socket (a followed chat, an open terminal, the Session
   * on screen, the app's state) is told again by those who keep it (the "moved" frame). A move that fails costs only
   * its own socket and backs off; a lost direct path afterwards is an honest reconnect like any other.
   */
  private scheduleMoveCheck(): void {
    if (this.moveTimer || this.moving || this.moveChecking || this.away) return;
    if (this.state !== "connected" || !this.socket || this.openedPath !== "relay") return;
    const wait = Math.max(this.moveWaitMs, this.moveBackoffUntil - Date.now());
    this.moveTimer = setTimeout(() => {
      this.moveTimer = null;
      void this.checkMove();
    }, wait);
  }

  private async checkMove(): Promise<void> {
    const socket = this.socket;
    if (!socket || this.state !== "connected" || this.openedPath !== "relay" || this.away || this.moving) return;
    this.moveChecking = true;
    let found: { url: string; path: string } | null = null;
    try {
      found = await this.moves.check(this.server);
    } catch {
      found = null;
    } finally {
      this.moveChecking = false;
    }
    // The socket ended, or the app went away, while it was checked.
    if (this.socket !== socket || this.state !== "connected" || this.away || this.moving) {
      if (found) this.moves.abandoned(this.server);
      return;
    }
    if (!found) {
      this.healthyChecks = 0;
      this.moveWaitMs = Math.min(this.moveWaitMs * 2, transportTiming.moveCheckMaxMs);
    } else {
      this.healthyChecks++;
      this.moveWaitMs = transportTiming.moveCheckMs;
      // A big message on its way stays on the socket it comes on: the move waits for the next check.
      if (this.healthyChecks >= transportTiming.moveHealthyChecks && Date.now() >= this.moveBackoffUntil && Date.now() > this.comingUntil) {
        return void this.startMove(found);
      }
    }
    this.scheduleMoveCheck();
  }

  private async startMove(found: { url: string; path: string }): Promise<void> {
    const attempt = this.attempt;
    const epoch = this.moveEpoch;
    const from = this.socket;
    let socket: SocketLike;
    // A computer held to its proof proves the move's socket too, over the move's own nonce, before the Transport moves
    // onto it: nothing goes out on it and nothing it sends is heard until then (security review 11-23, round 2 F3).
    const nonceHex = newChallenge();
    let held = false;
    this.moveChecking = true;
    try {
      held = await daemonProves(this.server.daemonPublicKey);
      const authorization = await buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: "mewla-connect", nonceHex, binding: await bindingFor(this.server, found.url) });
      if (epoch !== this.moveEpoch || attempt !== this.attempt || this.socket !== from || this.state !== "connected" || this.away) {
        return this.moves.abandoned(this.server);
      }
      socket = platform().openSocket(found.url, [authSubprotocol(authorization), ...frameSubprotocols()]);
    } catch {
      this.moveChecking = false;
      return this.failMove();
    } finally {
      this.moveChecking = false;
    }
    const timer = setTimeout(() => this.failMove(), transportTiming.moveOpenLimitMs);
    this.moving = { socket, timer, path: found.path };
    // Open and proved, in whichever order NSH tells them: then the Transport moves onto it, and what came behind the
    // proof is heard after the move, in order.
    let open = false;
    let proved = !held;
    const gate: Array<{ data: unknown }> = [];
    const ready = () => {
      if (!open || !proved || this.moving?.socket !== socket) return;
      if (attempt !== this.attempt || this.socket !== from || this.state !== "connected") return this.failMove();
      this.swap(socket, attempt);
      for (const event of gate.slice(1)) socket.onmessage?.(event);
    };
    socket.onmessage = (event) => {
      if (!held || this.moving?.socket !== socket) return;
      gate.push(event);
      if (gate.length > 1) return;
      void (async () => {
        let frame: Frame | null = null;
        try {
          const text = frameText(event.data);
          frame = text === null ? null : JSON.parse(text);
        } catch {
          frame = null;
        }
        const ok =
          frame?.type === "daemon_proof" &&
          (await proofAnswers({ purpose: CONNECT_PROOF_PURPOSE, challengeHex: nonceHex, daemonId: this.server.daemonId, daemonPublicKey: this.server.daemonPublicKey, proof: frame }));
        if (this.moving?.socket !== socket) return;
        if (!ok) return this.failMove();
        proved = true;
        ready();
      })();
    };
    socket.onopen = () => {
      if (this.moving?.socket !== socket) return socket.close(1000, "moved");
      open = true;
      ready();
    };
    socket.onerror = () => this.moving?.socket === socket && this.failMove();
    socket.onclose = () => this.moving?.socket === socket && this.failMove();
  }

  /** The move's socket did not open: it is closed, the relay connection is left as it was, and moves back off. */
  private failMove(): void {
    this.abandonMove("failed");
    this.backOffMoves();
    this.scheduleMoveCheck();
  }

  private backOffMoves(): void {
    this.healthyChecks = 0;
    this.moveBackoffUntil = Date.now() + this.moveBackoffMs;
    this.moveBackoffMs = Math.min(this.moveBackoffMs * 2, transportTiming.moveBackoffMaxMs);
  }

  /** Gives up a move under way (its socket, the tunnel found for it); the live socket is not touched. */
  private abandonMove(why: string): void {
    this.moveEpoch++;
    const moving = this.moving;
    if (!moving) return;
    this.moving = null;
    clearTimeout(moving.timer);
    moving.socket.onopen = moving.socket.onmessage = moving.socket.onerror = moving.socket.onclose = null;
    try {
      moving.socket.close(1000, why);
    } catch {
      // Closed already.
    }
    this.moves.abandoned(this.server);
  }

  /** The move's socket is open: the Transport is on it from now on, in one step, and the old one retires. */
  private swap(socket: SocketLike, attempt: number): void {
    const moving = this.moving!;
    this.moving = null;
    clearTimeout(moving.timer);
    const old = this.socket!;
    const fromPath = this.openedPath;
    const overlapFrom = this.openedAt;
    // Every answer still owed was asked on the old socket: it comes there.
    this.retire(old, new Set(this.asked.keys()));
    this.socket = socket;
    this.listen(socket, attempt);
    this.moves.moved(this.server);
    const now = Date.now();
    this.lastFrameAt = now;
    this.comingUntil = 0;
    this.openedAt = now;
    this.openedByMove = true;
    this.openedPath = this.moves.current(this.server) ?? moving.path;
    // The backoff starts over only once the direct path held (`end`): a path that keeps dying soon after a move is
    // moved onto ever more rarely, up to `moveBackoffMaxMs`.
    this.healthyChecks = 0;
    if (this.networkProof) clearTimeout(this.networkProof);
    this.networkProof = null;
    try {
      this.sendQuietly({
        type: "client_diagnostic",
        name: "link_move",
        request_id: `move-${now.toString(36)}`,
        body: `from=${fromPath} to=${this.openedPath} after=${((now - overlapFrom) / 1000).toFixed(1)}s owed=${this.retiring?.owed.size ?? 0}`,
      });
    } catch {
      // A report is not worth the move.
    }
    this.drain();
    // Those who keep something per socket on the computer tell it again, before the new socket's frames are handed over.
    this.receive({ type: "moved", path: this.openedPath }, BIG_FRAME_BYTES);
  }

  /**
   * The old socket of a move: only the first answer to each request asked on it is handed over (what else comes there,
   * the new socket brings too), then, or after `retireLimitMs`, it is closed with a normal close. Its end changes nothing.
   */
  private retire(socket: SocketLike, owed: Set<string>): void {
    this.endRetired();
    const timer = setTimeout(() => this.endRetired(), transportTiming.retireLimitMs);
    this.retiring = { socket, owed, timer };
    socket.onmessage = (event) => {
      const retiring = this.retiring;
      if (retiring?.socket !== socket) return;
      let frame: Frame;
      let bytes = 0;
      try {
        const text = frameText(event.data);
        if (text === null) return;
        bytes = text.length;
        frame = JSON.parse(text);
      } catch {
        return;
      }
      if (!frame || typeof frame.type !== "string" || typeof frame.request_id !== "string" || !retiring.owed.delete(frame.request_id)) return;
      this.receive(frame, bytes);
      if (retiring.owed.size === 0) this.endRetired();
    };
    socket.onerror = socket.onclose = () => {
      if (this.retiring?.socket === socket) this.endRetired();
    };
    if (owed.size === 0) this.endRetired();
  }

  private endRetired(): void {
    const retiring = this.retiring;
    if (!retiring) return;
    this.retiring = null;
    clearTimeout(retiring.timer);
    const socket = retiring.socket;
    socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
    try {
      socket.close(1000, "moved");
    } catch {
      // Closed already.
    }
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
export function requestError(frame: Frame, fallback: string): Error & { code?: string; frame?: Frame } {
  const error: Error & { code?: string; frame?: Frame } = new Error(String(frame.message || fallback));
  if (typeof frame.code === "string") error.code = frame.code;
  error.frame = frame;
  return error;
}

/** The text an `error` event carries (NSH sets `message`); empty when it carries none. */
function errorText(event: unknown): string {
  const message = (event as { message?: unknown } | null)?.message;
  return typeof message === "string" ? message.trim() : "";
}
