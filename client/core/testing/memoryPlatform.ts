import { createHash, randomBytes, randomUUID } from "node:crypto";
import { installPlatform, type KeyValueStore, type Platform, type SocketLike } from "../platform";

export function memoryStore(initial: Record<string, string> = {}): KeyValueStore & { entries: Map<string, string> } {
  const entries = new Map(Object.entries(initial));
  return {
    entries,
    getItem: async (key) => entries.get(key) ?? null,
    setItem: async (key, value) => void entries.set(key, value),
    removeItem: async (key) => void entries.delete(key),
  };
}

/** A platform for `bun test`: in-memory stores, Node's crypto, and the sockets and fetch a test gives it. */
export function installMemoryPlatform(overrides: Partial<Platform> = {}): Platform {
  const value: Platform = {
    os: "test",
    deviceName: () => "Test phone",
    randomBytes: (length) => new Uint8Array(randomBytes(length)),
    randomUUID: () => randomUUID(),
    sha256: async (data) => new Uint8Array(createHash("sha256").update(data).digest()),
    storage: memoryStore(),
    secure: memoryStore(),
    openSocket: () => {
      throw new Error("test: no socket");
    },
    fetch: () => Promise.reject(new Error("test: no network")),
    ...overrides,
  };
  installPlatform(value);
  return value;
}

/** A socket the test drives: it records what was sent and refuses past `quota` unsent frames, as NSH's does. */
export class FakeSocket implements SocketLike {
  readyState = 0;
  onopen: SocketLike["onopen"] = null;
  onmessage: SocketLike["onmessage"] = null;
  onclose: SocketLike["onclose"] = null;
  onerror: SocketLike["onerror"] = null;
  sent: string[] = [];
  /** Bytes sent in this turn, not yet gone out (NSH 0.5.0: up to 8 MiB in at most 1,024 messages wait). */
  buffered = 0;
  private waiting = 0;
  constructor(readonly url: string, readonly protocols: string[], readonly quotaBytes = 8 << 20, readonly quotaMessages = 1024) {}
  send(data: string): void {
    if (this.buffered + data.length > this.quotaBytes || this.waiting >= this.quotaMessages) {
      throw Object.assign(new Error("quota"), { code: "ERR_NSH_QUOTA_EXCEEDED" });
    }
    this.buffered += data.length;
    this.waiting++;
    this.sent.push(data);
    setTimeout(() => {
      this.buffered -= data.length;
      this.waiting--;
    }, 0);
  }
  close(code = 1000, reason = ""): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.({ code, reason });
  }
  open(): void {
    this.readyState = 1;
    this.onopen?.({});
  }
  receive(frame: object): void {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
}
