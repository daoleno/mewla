/**
 * What core needs from the device. The app installs the NSH implementation at
 * start (`ui/platform.ts`); tests install `testing/memoryPlatform.ts`. Core
 * never imports `nsh`, so it runs under `bun test` as plain TypeScript.
 */
export interface KeyValueStore {
  getItem(key: string): Promise<string | null>;
  setItem(key: string, value: string): Promise<void>;
  removeItem(key: string): Promise<void>;
}

/** The browser-shaped WebSocket every target provides (NSH's global, the browser's, a test fake). */
export interface SocketLike {
  readonly readyState: number;
  onopen: ((event: unknown) => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: { code: number; reason: string }) => void) | null;
  onerror: ((event: unknown) => void) | null;
  send(data: string): void;
  close(code?: number, reason?: string): void;
}

/**
 * Mewla Relay's pinned tunnels, which the hosts open (`link.*`): a loopback port whose every
 * connection is carried over TLS to host:port, trusting only the computer's pinned transport key.
 * `measure` dials once first and reports the handshake time; `on-demand` only listens.
 */
export interface LinkTunnels {
  start(key: string, host: string, port: number, pin: string, mode: "measure" | "on-demand"): Promise<{ port: number; rttMs: number }>;
  stop(key: string): Promise<void>;
  /** What a tunnel saw, for a pairing that did not finish; empty where there is nothing to say. */
  events(key: string): Promise<string[]>;
}

export interface Platform {
  os: "ios" | "android" | "web" | "desktop" | "test";
  /** The name a computer lists this device under. */
  deviceName(): string;
  randomBytes(length: number): Uint8Array;
  randomUUID(): string;
  sha256(data: Uint8Array): Promise<Uint8Array>;
  /** App data that may be read by anyone with the device's backups: servers, drafts, preferences. */
  storage: KeyValueStore;
  /** The device key: the Keychain on iOS, the Keystore on Android. */
  secure: KeyValueStore;
  openSocket(url: string, protocols: string[]): SocketLike;
  fetch(input: string, init?: RequestInit): Promise<Response>;
  /** Absent where no host carries them (a browser, an old build). */
  linkTunnels?: LinkTunnels;
}

let current: Platform | null = null;

export function platform(): Platform {
  if (!current) throw new Error("core: no platform installed");
  return current;
}

export function installPlatform(next: Platform): void {
  current = next;
}
