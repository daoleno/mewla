import { host } from "nsh";
import "../../testing/Harness";
import { installPlatform, platform, type SocketLike } from "../../../../core/platform";
import { RemoteSession } from "../RemoteView";

/**
 * The More harness, with the computer's viewer sockets played by the test. Each socket
 * the app opens posts `app.socketOpened` ({url}) and `app.socketClosed` ({url}) when the
 * app closes it; pings are answered with a pong `host.params.rttMs` old (default 20).
 * `app.socketPush` ({message}) sends a text message to every open socket, `app.socketEnd`
 * ({reason}) ends them as the computer would. `app.viewer` ({...message}) is what the
 * viewer page says to every session still connecting or live (`connected`, `stats`).
 */
const params = host.params as { rttMs?: number };
const open = new Set<FakeSocket>();

class FakeSocket implements SocketLike {
  readyState = 0;
  onopen: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  binaryType = "arraybuffer";

  constructor(readonly url: string) {
    open.add(this);
    setTimeout(() => {
      if (this.readyState !== 0) return;
      this.readyState = 1;
      host.post("app.socketOpened", { url: this.url });
      this.onopen?.({});
    }, 1);
  }

  send(data: string): void {
    if (this.readyState !== 1 || typeof data !== "string") return;
    const message = JSON.parse(data) as { type?: string; t?: number };
    if (message.type === "ping" && typeof message.t === "number") {
      const t = message.t - (params.rttMs ?? 20);
      setTimeout(() => this.readyState === 1 && this.onmessage?.({ data: JSON.stringify({ type: "pong", t }) }), 0);
    } else host.post("app.socketSent", { url: this.url, message });
  }

  close(): void {
    if (this.readyState > 1) return;
    this.readyState = 3;
    open.delete(this);
    host.post("app.socketClosed", { url: this.url });
  }

  end(reason: string): void {
    if (this.readyState > 1) return;
    this.readyState = 3;
    open.delete(this);
    this.onclose?.({ code: 1000, reason });
  }
}

const real = platform();
installPlatform({ ...real, openSocket: (url) => new FakeSocket(url) });

host.on("app.socketPush", (data) => {
  const { message } = data as { message: unknown };
  for (const socket of open) if (socket.readyState === 1) socket.onmessage?.({ data: JSON.stringify(message) });
});
host.on("app.socketEnd", (data) => {
  const { reason } = (data ?? {}) as { reason?: string };
  for (const socket of [...open]) socket.end(reason ?? "");
});

const sessions = new Set<RemoteSession>();
const connect = RemoteSession.prototype.connect;
RemoteSession.prototype.connect = function (this: RemoteSession) {
  sessions.add(this);
  connect.call(this);
};
host.on("app.viewer", (message) => {
  for (const session of sessions) if (session.phase() !== "lost") session.onViewer(JSON.stringify(message));
});
