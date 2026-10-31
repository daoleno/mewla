import { Platform } from "react-native";
import { buildAuthorizationHeader } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";
import type { PhoneNativeModule, PhoneStatus } from "../modules/mewla-phone/src";
import {
  PHONE_PROTOCOL_VERSION,
  parseDaemonFrame,
  type PhoneEventFrame,
  type PhoneHello,
  type PhoneStateFrame,
} from "./phoneProtocol";

export type PhoneLinkState = "connecting" | "online" | "offline" | "replaced" | "unsupported";

export interface PhoneLinkOptions {
  server: StoredServer;
  native: PhoneNativeModule;
  appVersion: string;
  onState(state: PhoneLinkState): void;
}

const BACKOFF_MS = [1_000, 2_000, 4_000, 8_000, 15_000];

// The daemon's /phone/device close codes (daemon/phone/protocol.go).
const CLOSE_REPLACED = 4001;
const CLOSE_UNSUPPORTED = 4002;

/**
 * The phone's signed WebSocket to its computer. The computer sends one request at a time; the
 * native side runs it and the result goes back on the same socket. Only a person's Labs switch
 * starts it, it reconnects by itself, and it stays up in the background through the foreground
 * service the native side holds.
 */
export class PhoneLink {
  private socket: WebSocket | null = null;
  private stopped = false;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private lastState = "";
  private nextTryAt = 0;
  private opening = false;
  private subscriptions: { remove(): void }[] = [];

  constructor(private readonly options: PhoneLinkOptions) {}

  start() {
    this.stopped = false;
    this.subscriptions.push(
      this.options.native.addListener("onStatus", (event) => this.relayStatus(event.json)),
      this.options.native.addListener("onEvent", (event) => this.relayEvent(event.json)),
    );
    void this.open();
  }

  stop() {
    this.stopped = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.subscriptions.forEach((subscription) => subscription.remove());
    this.subscriptions = [];
    const socket = this.socket;
    this.socket = null;
    socket?.close();
  }

  private async open() {
    const { server, onState } = this.options;
    if (this.stopped || this.opening || this.socket) return;
    this.opening = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    onState("connecting");
    try {
      const url = new URL(await resolveStoredServerURL(server));
      url.protocol = url.protocol === "https:" || url.protocol === "wss:" ? "wss:" : "ws:";
      url.pathname = "/phone/device";
      url.search = "";
      const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "mewla-phone-device" });
      if (this.stopped) return;
      const NativeWebSocket = WebSocket as unknown as {
        new (url: string, protocols: undefined, options: { headers: Record<string, string> }): WebSocket;
      };
      const socket = new NativeWebSocket(url.toString(), undefined, { headers: { Authorization: authorization } });
      this.socket = socket;
      socket.onopen = () => {
        this.attempt = 0;
        this.lastState = "";
        this.hello();
      };
      socket.onmessage = (event) => {
        if (typeof event.data === "string") void this.onFrame(event.data);
      };
      socket.onclose = (event) => this.closed(socket, event.code);
      socket.onerror = () => undefined;
    } catch {
      this.retry();
    } finally {
      this.opening = false;
    }
  }

  private closed(socket: WebSocket, code: number) {
    if (this.socket !== socket) return;
    this.socket = null;
    if (this.stopped) return;
    // Another phone took this computer's Phone use, or this App is older than the computer
    // understands: reconnecting would only repeat it.
    if (code === CLOSE_REPLACED || code === CLOSE_UNSUPPORTED) {
      this.stopped = true;
      this.options.onState(code === CLOSE_REPLACED ? "replaced" : "unsupported");
      return;
    }
    this.retry();
  }

  private retry() {
    if (this.stopped) return;
    this.options.onState("offline");
    const delay = BACKOFF_MS[Math.min(this.attempt, BACKOFF_MS.length - 1)];
    this.attempt += 1;
    this.nextTryAt = Date.now() + delay;
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.open(), delay);
  }

  private send(frame: object) {
    const socket = this.socket;
    if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(frame));
  }

  private hello() {
    const status = this.currentStatus();
    const hello: PhoneHello = {
      type: "hello",
      version: PHONE_PROTOCOL_VERSION,
      model: String(Platform.constants && "Model" in Platform.constants ? (Platform.constants as { Model?: string }).Model ?? "" : ""),
      sdk: status?.sdk ?? Number(Platform.Version),
      app_version: this.options.appVersion,
      state: status?.state ?? "setup",
    };
    this.send(hello);
    this.options.onState("online");
    if (status) this.relayStatus(JSON.stringify(status));
  }

  private currentStatus(): PhoneStatus | null {
    try {
      return JSON.parse(this.options.native.status()) as PhoneStatus;
    } catch {
      return null;
    }
  }

  private relayStatus(json: string) {
    let status: PhoneStatus;
    try {
      status = JSON.parse(json) as PhoneStatus;
    } catch {
      return;
    }
    const problem = status.state === "setup" ? status.problem ?? undefined : undefined;
    const key = `${status.state}:${status.w}:${status.h}:${problem ?? ""}`;
    if (key === this.lastState) return;
    this.lastState = key;
    const frame: PhoneStateFrame = { type: "state", state: status.state, w: status.w || undefined, h: status.h || undefined, problem };
    this.send(frame);
  }

  private relayEvent(json: string) {
    try {
      const frame = JSON.parse(json) as PhoneEventFrame | { type: "tick" };
      if (frame.type === "tick") {
        this.tick();
        return;
      }
      this.send(frame);
    } catch {
      // Not an event frame.
    }
  }

  /**
   * The native side's heartbeat while the link is down. JS timers do not run while the App is in
   * the background, so a retry waiting on one would wait until the person opens the App again.
   */
  private tick() {
    if (this.stopped || this.socket || this.opening || Date.now() < this.nextTryAt) return;
    void this.open();
  }

  private async onFrame(raw: string) {
    const frame = parseDaemonFrame(raw);
    if (!frame) return;
    if (frame.type !== "request") return;
    let result: string;
    try {
      result = await this.options.native.execute(raw);
    } catch (error) {
      result = JSON.stringify({
        type: "result",
        id: frame.id,
        ok: false,
        code: "failed",
        error: error instanceof Error ? error.message : "The phone could not do that",
      });
    }
    const socket = this.socket;
    if (socket && socket.readyState === WebSocket.OPEN) socket.send(result);
  }
}
