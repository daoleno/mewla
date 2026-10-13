import { Platform } from "react-native";
import { buildAuthorizationHeader } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";

export interface ScreenPower {
  source?: "ac" | "battery";
  percent?: number;
  charging?: boolean;
  lid_closed?: boolean;
  keeping_awake: boolean;
}

export interface ScreenStatus {
  enabled: boolean;
  available: boolean;
  platform: string;
  reason?: string;
  name?: string;
  width?: number;
  height?: number;
  keep_awake: boolean;
  power?: ScreenPower;
}

/** HTTP failure with the status kept apart from any body text. */
export class ScreenRequestError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
    this.name = "ScreenRequestError";
  }
}

async function endpoint(server: StoredServer, path: string) {
  const url = new URL(await resolveStoredServerURL(server));
  url.pathname = path;
  url.search = "";
  return url;
}

/** Reads screen status, or changes keep-awake when `keepAwake` is given. */
export async function screenRequest(
  server: StoredServer,
  keepAwake?: boolean,
  signal?: AbortSignal,
): Promise<ScreenStatus> {
  const url = await endpoint(server, "/screen");
  url.protocol = url.protocol === "wss:" ? "https:" : url.protocol === "ws:" ? "http:" : url.protocol;
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "mewla-screen" });
  if (signal?.aborted) throw new Error("Screen request cancelled");
  const post = keepAwake !== undefined;
  const response = await fetch(url.toString(), {
    method: post ? "POST" : "GET",
    signal,
    headers: { Authorization: authorization, Accept: "application/json", ...(post ? { "Content-Type": "application/json" } : {}) },
    body: post ? JSON.stringify({ keep_awake: keepAwake }) : undefined,
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
    throw new ScreenRequestError(result?.error || body.trim().slice(0, 300) || "Screen request failed", response.status);
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

  constructor(private server: StoredServer, private handlers: ScreenConnectionHandlers) {}

  async connect() {
    const url = await endpoint(this.server, "/screen/viewer");
    url.protocol = url.protocol === "https:" || url.protocol === "wss:" ? "wss:" : "ws:";
    const authorization = await buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: "mewla-screen-view" });
    if (this.closed) return;
    let socket: WebSocket;
    if (Platform.OS === "web") {
      url.searchParams.set("auth", base64URL(authorization));
      socket = new WebSocket(url.toString());
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
      let message: { type?: string; t?: number; error?: string };
      try { message = JSON.parse(event.data); } catch { return; }
      if (message.type === "pong" && typeof message.t === "number") this.handlers.rtt(Date.now() - message.t);
      else if (message.type === "ready") this.handlers.ready();
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

function base64URL(value: string): string {
  return btoa(value).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}
