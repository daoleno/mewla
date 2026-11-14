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
  firstRetryMs: 1_000,
  maxRetryMs: 30_000,
  /** On Mewla Relay's relay, how often to look for a direct path (a phone that joined the computer's Wi-Fi). */
  directCheckMs: 20_000,
};

/** Resolves the socket URL for a server: the stored address, or a local tunnel the host opens (Mewla Relay). */
export type URLResolver = (server: StoredServer) => Promise<string>;

type Listener = (frame: Frame) => void;

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
  private listeners = new Map<string, Set<Listener>>();
  private stateListeners = new Set<(state: ConnectionState) => void>();
  private wanted = false;
  private attempt = 0;
  private retryMs = transportTiming.firstRetryMs;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private watchdog: ReturnType<typeof setInterval> | null = null;
  private directCheck: ReturnType<typeof setInterval> | null = null;
  private lastFrameAt = 0;
  private nextId = 0;

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
    socket?.close(1000, "closed");
    // A computer that is no longer current keeps no tunnels open; coming back measures afresh.
    void invalidateStoredServerTransport(this.server);
    this.setState("idle");
    this.emit({ type: "disconnected", intentional: true });
  }

  /** Back in the foreground or on a new network: skip the backoff. */
  resume(): void {
    if (!this.wanted || this.state === "connected" || this.state === "connecting") return;
    this.retryMs = transportTiming.firstRetryMs;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    void this.open();
  }

  /** Replaces a socket that stopped answering: closes it as lost and reconnects without waiting. */
  drop(reason: string): void {
    if (!this.socket) return;
    this.retryMs = transportTiming.firstRetryMs;
    this.kill(reason);
  }

  on(type: string, listener: Listener): () => void {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(listener);
    return () => set.delete(listener);
  }

  onState(listener: (state: ConnectionState) => void): () => void {
    this.stateListeners.add(listener);
    return () => this.stateListeners.delete(listener);
  }

  /** Sends now or after the frames already waiting; throws only when not connected. */
  send(frame: Frame): void {
    if (this.state !== "connected" || !this.socket) throw new Error("Daemon is not connected.");
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
      stops.push(this.on("disconnected", () => finish(() => reject(new Error("Daemon is not connected.")))));
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

  private async open(): Promise<void> {
    const attempt = ++this.attempt;
    this.setState("connecting");
    let socket: SocketLike;
    try {
      const [authorization, url] = await Promise.all([
        buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: "mewla-connect" }),
        this.resolveURL(this.server),
      ]);
      if (attempt !== this.attempt) return;
      socket = platform().openSocket(url, [authSubprotocol(authorization), ...frameSubprotocols()]);
    } catch (error) {
      if (attempt === this.attempt) {
        void transportFailed(this.server);
        this.retry(error instanceof Error ? error.message : String(error));
      }
      return;
    }
    this.socket = socket;
    let ended = false;
    socket.onopen = () => {
      if (attempt !== this.attempt) return socket.close();
      this.retryMs = transportTiming.firstRetryMs;
      this.problem = "";
      this.lastFrameAt = Date.now();
      this.startWatchdog();
      this.setState("connected");
      this.emit({ type: "connected" });
    };
    socket.onmessage = (event) => {
      if (attempt !== this.attempt) return;
      this.lastFrameAt = Date.now();
      let frame: Frame;
      try {
        const text = frameText(event.data);
        if (text === null) return;
        frame = JSON.parse(text);
      } catch {
        return;
      }
      if (frame && typeof frame.type === "string") this.emit(frame);
    };
    const end = (why: string) => {
      if (ended) return;
      ended = true;
      if (this.socket === socket) this.socket = null;
      if (attempt !== this.attempt) return;
      this.clearTimers();
      this.queue = [];
      const wasConnected = this.state === "connected";
      if (wasConnected) this.emit({ type: "disconnected", intentional: false });
      // Moving onto a direct path keeps the relay open as the fallback; any other end measures afresh.
      if (why !== "direct-available") void transportFailed(this.server);
      this.retry(why);
    };
    socket.onerror = () => end("error");
    socket.onclose = (event) => end(event.reason || `closed (${event.code})`);
  }

  private retry(problem: string): void {
    this.problem = problem;
    if (!this.wanted) return this.setState("idle");
    this.setState("waiting");
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.open();
    }, this.retryMs);
    this.retryMs = Math.min(this.retryMs * 2, transportTiming.maxRetryMs);
  }

  private startWatchdog(): void {
    let lastTick = Date.now();
    this.watchdog = setInterval(() => {
      const now = Date.now();
      // A late tick means the app itself was away; frames may be queued behind it.
      const away = now - lastTick > transportTiming.checkMs * 3;
      lastTick = now;
      if (!away && now - this.lastFrameAt > transportTiming.silentLimitMs) this.kill("silent");
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

  private clearTimers(): void {
    if (this.watchdog) clearInterval(this.watchdog);
    if (this.directCheck) clearInterval(this.directCheck);
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.watchdog = null;
    this.directCheck = null;
    this.retryTimer = null;
  }

  private setState(state: ConnectionState): void {
    if (this.state === state) return;
    this.state = state;
    this.stateListeners.forEach((listener) => listener(state));
  }

  private emit(frame: Frame): void {
    this.listeners.get(frame.type)?.forEach((listener) => listener(frame));
  }
}

/** A daemon error reply as an Error that keeps its `code` (`answer_choice_gone`, `input_uncertain`, …). */
export function requestError(frame: Frame, fallback: string): Error & { code?: string } {
  const error: Error & { code?: string } = new Error(String(frame.message || fallback));
  if (typeof frame.code === "string") error.code = frame.code;
  return error;
}
