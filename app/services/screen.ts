import { Platform } from "react-native";
import { buildAuthorizationHeader } from "./auth";
import { resolveStoredServerURL, transportPathOfURL, type TransportPath } from "./pinnedTransport";
import { screenFailureText } from "./screenFailure";
import { parseStreamStats, type ScreenStreamStats } from "./screenStream";
import type { StoredServer } from "./storage";
import { authSubprotocol } from "./webSocketAuth";

export interface ScreenPower {
  source?: "ac" | "battery";
  percent?: number;
  charging?: boolean;
  lid_closed?: boolean;
  keeping_awake: boolean;
}

export interface ScreenPermission {
  id: string;
  name: string;
  granted: boolean;
  guidance?: string;
  /** The daemon can open this permission's settings page on the computer. */
  can_open?: boolean;
  /** Granted, but takes effect only after Mewla restarts on the computer. */
  restart_required?: boolean;
}

/** One computer's Screen state, as the asking device sees it. */
export interface ScreenStatus {
  supported: boolean;
  platform: string;
  reason?: string;
  /** The computer's own name. */
  name?: string;
  /** Turned off at the computer with `mewla screen disable`. */
  disabled: boolean;
  /** This device was allowed at the computer. */
  approved: boolean;
  permissions?: ScreenPermission[];
  ready: boolean;
  keep_awake: boolean;
  power?: ScreenPower;
  /** A service manager restarts Mewla, so the app may ask it to restart. */
  can_restart?: boolean;
  /** An agent using the screen now. Viewing it takes over and stops the agent. */
  agent?: { worker: string; name?: string; since: string };
  /** The computer is locked: a viewer sees its login window. */
  locked?: boolean;
}

export type ScreenAction =
  | { action: "enable" }
  | { action: "open_settings"; permission: string }
  | { action: "restart" }
  | { action: "stop_agent" }
  | { keep_awake: boolean };

/** HTTP failure with the status kept apart from any body text. */
export class ScreenRequestError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
    this.name = "ScreenRequestError";
  }
}

async function endpoint(server: StoredServer, path: string) {
  const url = new URL(await resolveStoredServerURL(server));
  const via = transportPathOfURL(server.id, url.toString());
  url.pathname = path;
  url.search = "";
  return Object.assign(url, { via });
}

/** Reads Screen status, or runs one action and returns the new status. */
export async function screenRequest(
  server: StoredServer,
  action?: ScreenAction,
  signal?: AbortSignal,
): Promise<ScreenStatus> {
  const url = await endpoint(server, "/screen");
  url.protocol = url.protocol === "wss:" ? "https:" : url.protocol === "ws:" ? "http:" : url.protocol;
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "mewla-screen" });
  if (signal?.aborted) throw new Error("Screen request cancelled");
  const response = await fetch(url.toString(), {
    method: action ? "POST" : "GET",
    signal,
    headers: { Authorization: authorization, Accept: "application/json", ...(action ? { "Content-Type": "application/json" } : {}) },
    body: action ? JSON.stringify(action) : undefined,
  });
  const body = await response.text();
  let result: (ScreenStatus & { error?: string }) | undefined;
  try { result = body ? JSON.parse(body) : undefined; } catch { result = undefined; }
  // An older daemon answers 404, or its web UI answers with HTML.
  const isJSON = (response.headers.get("content-type") || "").includes("application/json");
  if (response.status === 404 || (response.ok && !isJSON)) {
    throw new ScreenRequestError("This server's Mewla is too old for Screen. Update it on that computer.", response.status);
  }
  if (!response.ok || !result) {
    throw new ScreenRequestError(result?.error || screenFailureText(response.status, body), response.status);
  }
  return result;
}

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode.apply(null, Array.from(bytes.subarray(i, i + 0x8000)));
  }
  return btoa(binary);
}

export function base64ToBytes(text: string): Uint8Array {
  const binary = atob(text);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

export interface ScreenConnectionHandlers {
  /** The socket is open; the viewer may start its RFB client. */
  open(): void;
  /** RFB bytes from the screen, base64. */
  data(chunk: string): void;
  /** The daemon finished the RFB handshake on the viewer's behalf. */
  ready(): void;
  rtt(ms: number): void;
  /** The computer is locked, or not: sent when the viewer opens and on a change. */
  locked?(locked: boolean): void;
  /** The daemon's measurements of the stream, about once a second. */
  streamStats?(stats: ScreenStreamStats): void;
  /** The socket ended; `reason` is the daemon's explanation when it gave one. */
  closed(reason: string | null): void;
}

/**
 * One authenticated screen socket on one canonical server. Binary messages
 * carry RFB bytes; text messages carry JSON control. No automatic reconnect.
 */
export class ScreenConnection {
  private socket: WebSocket | null = null;
  private closed = false;
  private timer: ReturnType<typeof setInterval> | undefined;
  private reason: string | null = null;

  /** watch: see the agent using the screen without taking over; input goes nowhere. */
  constructor(private server: StoredServer, private handlers: ScreenConnectionHandlers, private watch = false) {}

  /** The path this viewer's socket is carried on, known once it is dialed. */
  path: TransportPath | null = null;

  async connect() {
    const url = await endpoint(this.server, "/screen/viewer");
    this.path = url.via;
    url.protocol = url.protocol === "https:" || url.protocol === "wss:" ? "wss:" : "ws:";
    if (this.watch) url.searchParams.set("watch", "1");
    const authorization = await buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: "mewla-screen-view" });
    if (this.closed) return;
    let socket: WebSocket;
    if (Platform.OS === "web") {
      socket = new WebSocket(url.toString(), [authSubprotocol(authorization)]);
    } else {
      const NativeWebSocket = WebSocket as unknown as { new(url: string, protocols: undefined, options: { headers: Record<string, string> }): WebSocket };
      socket = new NativeWebSocket(url.toString(), undefined, { headers: { Authorization: authorization } });
    }
    socket.binaryType = "arraybuffer";
    this.socket = socket;
    socket.onopen = () => {
      if (this.closed) return;
      this.handlers.open();
      const ping = () => {
        if (!this.closed && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "ping", t: Date.now() }));
      };
      ping();
      this.timer = setInterval(ping, 2000);
    };
    socket.onmessage = (event) => {
      if (this.closed) return;
      if (typeof event.data !== "string") {
        this.handlers.data(bytesToBase64(new Uint8Array(event.data as ArrayBuffer)));
        return;
      }
      let message: { type?: string; t?: number; error?: string; locked?: unknown };
      try { message = JSON.parse(event.data); } catch { return; }
      if (message.type === "pong" && typeof message.t === "number") this.handlers.rtt(Date.now() - message.t);
      else if (message.type === "ready") this.handlers.ready();
      else if (message.type === "state" && typeof message.locked === "boolean") this.handlers.locked?.(message.locked);
      else if (message.type === "stats") {
        const stats = parseStreamStats(message as Record<string, unknown>);
        if (stats) this.handlers.streamStats?.(stats);
        // The lock rides on stats too, in case a state message was dropped.
        if (typeof message.locked === "boolean") this.handlers.locked?.(message.locked);
      }
      else if (message.type === "error" && typeof message.error === "string") this.reason = message.error;
    };
    socket.onerror = () => {
      if (!this.closed) this.finish();
    };
    socket.onclose = (event) => {
      if (!this.reason && event.reason) this.reason = event.reason;
      if (!this.closed) this.finish();
    };
  }

  /** RFB bytes from the viewer, base64. */
  send(chunk: string) {
    if (!this.closed && this.socket?.readyState === WebSocket.OPEN) this.socket.send(base64ToBytes(chunk).buffer as ArrayBuffer);
  }

  private finish() {
    const reason = this.reason;
    this.close();
    this.handlers.closed(reason);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    if (this.timer) clearInterval(this.timer);
    const socket = this.socket;
    this.socket = null;
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) socket.close();
  }
}
