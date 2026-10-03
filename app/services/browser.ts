import { buildAuthorizationHeader } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";

export interface BrowserResource {
  id: string;
  name: string;
  state: string;
  control: string;
  allow_agents: boolean;
  generation?: string;
  target?: string;
}
export interface BrowserLease {
  epoch: number;
  generation: string;
  target: string;
  kind: string;
  expires_at: string;
}
export interface BrowserCommand {
  kind: string;
  url?: string;
  target?: string;
  text?: string;
  input?: BrowserInput;
}
export interface BrowserInput {
  kind: string;
  x?: number;
  y?: number;
  dx?: number;
  dy?: number;
  button?: string;
  key?: string;
  text?: string;
}
export interface BrowserResponse {
  resources?: BrowserResource[];
  resource?: BrowserResource;
  capability?: { available: boolean; reason?: string };
  lease?: BrowserLease;
  result?: unknown;
}
export interface BrowserFrame {
  type: "frame";
  seq: number;
  data: string;
  metadata: { deviceWidth: number; deviceHeight: number };
}
/** HTTP failure with the status kept apart from any body text, so callers can explain it. */
export class BrowserRequestError extends Error {
  constructor(message: string, readonly status: number, readonly detail: string) {
    super(message);
    this.name = "BrowserRequestError";
  }
}
async function endpoint(server: StoredServer, path: string) {
  const url = new URL(await resolveStoredServerURL(server));
  url.pathname = path;
  url.search = "";
  return url;
}
export async function browserRequest(
  server: StoredServer,
  request: { action: string; id?: string; name?: string; allow_agents?: boolean },
  signal?: AbortSignal,
): Promise<BrowserResponse> {
  const url = await endpoint(server, "/browser");
  url.protocol = url.protocol === "wss:" ? "https:" : url.protocol === "ws:" ? "http:" : url.protocol;
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-browser" });
  if (signal?.aborted) throw new Error("Browser request cancelled");
  const response = await fetch(url.toString(), {
    method: "POST", signal,
    headers: { Authorization: authorization, "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  // Servers without Browser, proxies and auth failures answer in plain text.
  const body = await response.text();
  let result: (BrowserResponse & { error?: string }) | undefined;
  try { result = body ? JSON.parse(body) : undefined; } catch { result = undefined; }
  if (!response.ok || !result) {
    throw new BrowserRequestError(result?.error || "Browser request failed", response.status, result?.error || body.trim().slice(0, 300));
  }
  return result;
}

// One resource and one canonical server per instance. No automatic retake on reconnect.
export class BrowserViewer {
  private socket: WebSocket | null = null;
  private closed = false;
  private serial = 0;
  private revision = 0;
  private lease: BrowserLease | undefined;
  private timer: ReturnType<typeof setInterval> | undefined;
  private pending = new Map<string, {
    revision: number;
    resolve(value: BrowserResponse): void;
    reject(error: Error): void;
    timer: ReturnType<typeof setTimeout>;
  }>();
  private tail: Promise<unknown> = Promise.resolve();
  private queued = 0;
  constructor(
    private server: StoredServer,
    private id: string,
    private handlers: {
      frame(frame: BrowserFrame): void;
      status(value: string): void;
      resource(value: BrowserResource): void;
      /** Socket liveness; status strings stay informational. */
      connection?(state: "live" | "lost"): void;
      /** Whether this viewer holds a server-issued input lease. */
      lease?(held: boolean): void;
    },
  ) {}
  async connect() {
    const url = await endpoint(this.server, "/browser/viewer");
    url.protocol = url.protocol === "https:" || url.protocol === "wss:" ? "wss:" : "ws:";
    url.searchParams.set("id", this.id);
    const authorization = await buildAuthorizationHeader({ daemonId: this.server.daemonId, purpose: `zen-browser-view:${this.id}` });
    if (this.closed) return;
    const NativeWebSocket = WebSocket as unknown as { new(url: string, protocols: undefined, options: { headers: Record<string, string> }): WebSocket };
    const socket = new NativeWebSocket(url.toString(), undefined, { headers: { Authorization: authorization } });
    this.socket = socket;
    socket.onopen = () => {
      if (this.closed) return;
      this.handlers.status("Viewing");
      this.handlers.connection?.("live");
      // Fresh control state for this connection; cached state may predate a drop.
      socket.send(JSON.stringify({ type: "ping" }));
      this.timer = setInterval(() => {
        if (this.closed || socket.readyState !== WebSocket.OPEN) return;
        socket.send(JSON.stringify({ type: "ping" }));
        if (this.lease) void this.command({ kind: "heartbeat" }).catch(() => {});
      }, 10000);
    };
    socket.onmessage = (event) => {
      if (this.closed || typeof event.data !== "string" || event.data.length > 3000000) return;
      let message;
      try { message = JSON.parse(event.data); } catch { return; }
      if (message.type === "frame") {
        if (Number.isSafeInteger(message.seq) && typeof message.data === "string" && /^[A-Za-z0-9+/=]+$/.test(message.data)) this.handlers.frame(message);
      } else if (message.type === "response") {
        const pending = this.pending.get(message.request_id);
        if (!pending) return;
        clearTimeout(pending.timer);
        this.pending.delete(message.request_id);
        if (pending.revision !== this.revision) { pending.reject(new Error("Control changed")); return; }
        if (message.error) {
          if (this.lease) this.handlers.lease?.(false);
          this.lease = undefined;
          this.handlers.status(message.error);
          pending.reject(new Error(message.error));
        } else {
          const result: BrowserResponse = message.response;
          if (result.lease) {
            if (!this.lease) this.handlers.lease?.(true);
            this.lease = result.lease;
          }
          if (result.resource) this.handlers.resource(result.resource);
          pending.resolve(result);
        }
      } else if (message.type === "pong" && message.resource) this.handlers.resource(message.resource);
      else if (message.type === "error") this.handlers.status(message.error);
    };
    socket.onerror = () => {
      if (this.closed) return;
      this.handlers.status("Connection failed. Reconnect to view this browser.");
      this.handlers.connection?.("lost");
    };
    socket.onclose = () => {
      if (!this.closed) {
        this.handlers.status("Disconnected. Your browser is still on the server.");
        this.handlers.connection?.("lost");
      }
      this.close();
    };
  }
  ack(seq: number) {
    if (!this.closed && this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify({ type: "ack", seq }));
  }
  private request(type: string, command?: BrowserCommand): Promise<BrowserResponse> {
    if (this.closed || this.socket?.readyState !== WebSocket.OPEN) return Promise.reject(new Error("Viewer is disconnected"));
    const request_id = String(++this.serial);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(request_id);
        reject(new Error("Browser response timed out. Reconnect before continuing."));
        if (!this.closed) this.handlers.connection?.("lost");
        this.close();
      }, 15000);
      this.pending.set(request_id, { resolve, reject, timer, revision: this.revision });
      this.socket!.send(JSON.stringify({ type, request_id, lease: this.lease, command }));
    });
  }
  control() { this.revision++; this.lease = undefined; return this.request("control"); }
  async release() {
    this.revision++;
    this.lease = undefined;
    const response = await this.request("release");
    this.handlers.status("Viewing · Agent may request control");
    return response;
  }
  command(command: BrowserCommand): Promise<BrowserResponse> {
    if (this.queued >= 32) return Promise.reject(new Error("Input is busy; wait a moment"));
    this.queued++;
    const revision = this.revision;
    const next = this.tail.then(() => {
      if (revision !== this.revision || !this.lease) throw new Error("Take control before interacting");
      return this.request("command", command);
    });
    this.tail = next.catch(() => {}).finally(() => { this.queued--; });
    return next;
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.lease = undefined;
    clearInterval(this.timer);
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new Error("Viewer closed"));
    }
    this.pending.clear();
    if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify({ type: "close" }));
    this.socket?.close(1000, "viewer detached");
    this.socket = null;
  }
}
