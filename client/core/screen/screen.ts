import { buildHTTPURL } from "../pairing";
import { daemonURL, deviceAuthorization, signedFetch } from "../more/daemon";
import { platform, type SocketLike } from "../platform";
import type { StoredServer } from "../storedServerContract";
import { authSubprotocol } from "../webSocketAuth";
import { screenFailureText } from "./screenFailure";
import { parseStreamStats, type ScreenStreamStats } from "./screenStream";

/**
 * Screen: the computer's own screen (Mac, Windows) or an agent's display (Linux),
 * over the computer's HTTP doors `/screen` and `/display` and two viewer sockets
 * that carry RFB bytes (binary) beside JSON control (text).
 */

export interface ScreenPower {
  source?: "ac" | "battery";
  percent?: number;
  charging?: boolean;
  lid_closed?: boolean;
  keeping_awake: boolean;
  display_kept_for_agents?: boolean;
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
  agent?: ScreenAgent;
  /** Every agent using the screen, longest first. */
  agents?: ScreenAgent[];
  /** The computer is locked: a viewer sees its login window. */
  locked?: boolean;
  /** The asking device is on this computer itself: a live view would only mirror the viewer. */
  local?: boolean;
  /** Agents here each use a display of their own (Linux), listed in Screen one by one. */
  agent_displays?: boolean;
}

export interface ScreenAgent {
  worker: string;
  name?: string;
  since: string;
}

export type ScreenAction =
  | { action: "enable" }
  | { action: "open_settings"; permission: string }
  | { action: "restart" }
  | { action: "stop_agent"; worker?: string }
  | { keep_awake: boolean };

/** HTTP failure with the status kept apart from any body text. */
export class ScreenRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ScreenRequestError";
  }
}

async function readAnswer(response: Response): Promise<{ result: Record<string, unknown> | undefined; body: string; json: boolean }> {
  const body = await response.text();
  let result: Record<string, unknown> | undefined;
  try {
    result = body ? (JSON.parse(body) as Record<string, unknown>) : undefined;
  } catch {
    result = undefined;
  }
  return { result, body, json: (response.headers.get("content-type") || "").includes("application/json") };
}

/** Reads Screen status, or runs one action and returns the new status. */
export async function screenRequest(server: StoredServer, action?: ScreenAction): Promise<ScreenStatus> {
  const response = await signedFetch(server, action ? "POST" : "GET", "/screen", "mewla-screen", { body: action, timeoutMs: action && "action" in action && action.action === "enable" ? 300_000 : undefined });
  const { result, body, json } = await readAnswer(response);
  // An older daemon answers 404, or its web UI answers with HTML.
  if (response.status === 404 || (response.ok && !json)) throw new ScreenRequestError("This server's Mewla is too old for Screen. Update it on that computer.", response.status);
  if (!response.ok || !result) throw new ScreenRequestError((typeof result?.error === "string" && result.error) || screenFailureText(response.status, body), response.status);
  return result as unknown as ScreenStatus;
}

/** One agent's private display on a Linux computer: desktop apps it opened, off the person's screen. */
export interface AgentDisplay {
  /** The Worker it belongs to. */
  id: string;
  width: number;
  height: number;
  /** Who holds it now: the person while they hold it, otherwise the agent. */
  control: "agent" | "person";
  /** The person stopped this agent; it stays stopped until they resume it. */
  stopped?: boolean;
  apps: number;
  /** Seconds since the agent last used it; 0 while it acts. */
  idle_s?: number;
}

export interface DisplayList {
  displays: AgentDisplay[];
  /** Whether this computer can run displays at all. */
  available: boolean;
  /** What is missing when agents cannot have a screen or a browser on it here. */
  reason?: string;
}

async function displayRequest(server: StoredServer, body?: object): Promise<Record<string, unknown>> {
  const response = await signedFetch(server, body ? "POST" : "GET", "/display", "mewla-display", { body });
  const { result, json } = await readAnswer(response);
  if (response.status === 404 || response.status === 503 || (response.ok && !json)) throw new ScreenRequestError("This server does not run agent displays.", response.status);
  if (!response.ok || !result) throw new ScreenRequestError(typeof result?.error === "string" ? result.error : "Display request failed", response.status);
  return result;
}

export async function listDisplays(server: StoredServer): Promise<DisplayList> {
  const result = await displayRequest(server);
  return {
    displays: Array.isArray(result.displays) ? (result.displays as AgentDisplay[]) : [],
    available: result.available !== false,
    reason: typeof result.reason === "string" ? result.reason : "",
  };
}

/** Stop: the agent may not use its display until the person resumes it. */
export async function haltDisplay(server: StoredServer, worker: string): Promise<void> {
  await displayRequest(server, { action: "halt", worker });
}

/** Lifts a Stop: the agent may use its display again. */
export async function resumeDisplay(server: StoredServer, worker: string): Promise<void> {
  await displayRequest(server, { action: "resume", worker });
}

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode.apply(null, Array.from(bytes.subarray(i, i + 0x8000)));
  return btoa(binary);
}

export function base64ToBytes(text: string): Uint8Array {
  const binary = atob(text);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

/** A viewer socket other than the computer's own screen. */
export interface ScreenRoute {
  path: string;
  purpose: "mewla-display-view";
  params: Record<string, string>;
}

/** The ws(s) address of a viewer socket, on the address the app's own socket uses. */
export function viewerURL(server: StoredServer, path: string, params: Record<string, string>): string {
  const http = buildHTTPURL(server.url, path) ?? daemonURL(server, path);
  const url = new URL(http);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
  return url.toString();
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

const OPEN = 1;
const CONNECTING = 0;

/**
 * One authenticated screen socket on the current computer. The device key signs
 * it as a WebSocket subprotocol. No automatic reconnect.
 */
export class ScreenConnection {
  private socket: SocketLike | null = null;
  private closed = false;
  private timer: ReturnType<typeof setInterval> | undefined;
  private reason: string | null = null;

  /**
   * watch: see the agent using the screen without taking over; input goes nowhere.
   * route: another viewer socket that speaks the same RFB, such as an agent's display.
   */
  constructor(
    private server: StoredServer,
    private handlers: ScreenConnectionHandlers,
    private watch = false,
    private worker?: string,
    private route?: ScreenRoute,
  ) {}

  async connect(): Promise<void> {
    const params: Record<string, string> = { ...(this.route?.params ?? {}) };
    if (!this.route && this.watch) params.watch = "1";
    if (!this.route && this.watch && this.worker) params.worker = this.worker;
    const url = viewerURL(this.server, this.route?.path ?? "/screen/viewer", params);
    const authorization = await deviceAuthorization(this.server, this.route?.purpose ?? "mewla-screen-view");
    if (this.closed) return;
    let socket: SocketLike;
    try {
      socket = platform().openSocket(url, [authSubprotocol(authorization)]);
    } catch (error) {
      this.reason = error instanceof Error ? error.message : null;
      this.finish();
      return;
    }
    (socket as SocketLike & { binaryType?: string }).binaryType = "arraybuffer";
    this.socket = socket;
    socket.onopen = () => {
      if (this.closed) return;
      this.handlers.open();
      const ping = () => this.enqueue(JSON.stringify({ type: "ping", t: Date.now() }));
      ping();
      this.timer = setInterval(ping, 2000);
    };
    socket.onmessage = (event) => {
      if (this.closed) return;
      if (typeof event.data !== "string") {
        const data = event.data instanceof ArrayBuffer ? new Uint8Array(event.data) : ArrayBuffer.isView(event.data) ? new Uint8Array((event.data as ArrayBufferView).buffer) : null;
        if (data) this.handlers.data(bytesToBase64(data));
        return;
      }
      let message: { type?: string; t?: number; error?: string; locked?: unknown };
      try {
        message = JSON.parse(event.data);
      } catch {
        return;
      }
      if (message.type === "pong" && typeof message.t === "number") this.handlers.rtt(Date.now() - message.t);
      else if (message.type === "ready") this.handlers.ready();
      else if (message.type === "state" && typeof message.locked === "boolean") this.handlers.locked?.(message.locked);
      else if (message.type === "stats") {
        const stats = parseStreamStats(message as Record<string, unknown>);
        if (stats) this.handlers.streamStats?.(stats);
        // The lock rides on stats too, in case a state message was dropped.
        if (typeof message.locked === "boolean") this.handlers.locked?.(message.locked);
      } else if (message.type === "error" && typeof message.error === "string") this.reason = message.error;
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
  send(chunk: string): void {
    this.enqueue(base64ToBytes(chunk).buffer as ArrayBuffer);
  }

  /** A control message beside the RFB bytes, such as {type:"hold",on:true}. */
  control(message: Record<string, unknown>): void {
    this.enqueue(JSON.stringify(message));
  }

  private outbox: Array<string | ArrayBuffer> = [];
  private draining = false;

  /** NSH holds a few queued sends per socket; the rest wait here, in order. */
  private enqueue(data: string | ArrayBuffer): void {
    if (this.closed || this.socket?.readyState !== OPEN) return;
    this.outbox.push(data);
    this.drain();
  }

  private drain(): void {
    if (this.draining) return;
    const socket = this.socket as unknown as { readyState: number; send(data: string | ArrayBuffer): void } | null;
    while (socket && !this.closed && this.outbox.length) {
      try {
        socket.send(this.outbox[0]);
        this.outbox.shift();
      } catch (error) {
        if ((error as { code?: string })?.code !== "ERR_NSH_QUOTA_EXCEEDED") {
          this.outbox = [];
          return;
        }
        this.draining = true;
        setTimeout(() => {
          this.draining = false;
          this.drain();
        }, 1);
        return;
      }
    }
  }

  private finish(): void {
    const reason = this.reason;
    this.close();
    this.handlers.closed(reason);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.outbox = [];
    if (this.timer) clearInterval(this.timer);
    const socket = this.socket;
    this.socket = null;
    if (socket && (socket.readyState === OPEN || socket.readyState === CONNECTING)) socket.close();
  }
}

/** Whether this computer's Workers get a browser on its virtual screen (Linux). */
export async function virtualScreen(server: StoredServer, enabled?: boolean): Promise<{ enabled: boolean; unsupported: boolean }> {
  const response = await signedFetch(server, "POST", "/browser", "mewla-browser", { body: enabled === undefined ? { action: "settings" } : { action: "settings", enabled }, timeoutMs: 20_000 });
  const { result, body } = await readAnswer(response);
  if (!response.ok || !result) throw new ScreenRequestError((typeof result?.error === "string" && result.error) || body.trim().slice(0, 300) || "Browser request failed", response.status);
  if (typeof result.virtual_screen !== "boolean") throw new Error("Browser request failed");
  return { enabled: result.virtual_screen, unsupported: result.virtual_screen_unsupported === true };
}
