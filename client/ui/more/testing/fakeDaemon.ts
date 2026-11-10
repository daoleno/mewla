import type { Daemon } from "../../../core/more/daemon";
import type { StoredServer } from "../../../core/storedServerContract";
import type { ConnectionState, Frame, RequestOptions } from "../../../core/transport";

export type Answer = Record<string, unknown> | ((body: Record<string, unknown>) => Record<string, unknown> | Promise<Record<string, unknown>>);

/**
 * A computer for page tests: answers requests by frame type from `answers`
 * (an answer that throws or an `Error` value fails the request), records what
 * was asked, and can push frames and change its connection state.
 */
export class FakeDaemon implements Daemon {
  state: ConnectionState;
  readonly asked: Array<{ type: string; body: Record<string, unknown> }> = [];
  readonly sent: Frame[] = [];
  private listeners = new Map<string, Set<(frame: Frame) => void>>();
  private stateListeners = new Set<(state: ConnectionState) => void>();

  constructor(
    readonly answers: Record<string, Answer | Error> = {},
    readonly server: StoredServer = testServer(),
    state: ConnectionState = "connected",
  ) {
    this.state = state;
  }

  async request<T = Frame>(type: string, body: Record<string, unknown>, options: RequestOptions): Promise<T> {
    this.asked.push({ type, body });
    if (this.state !== "connected") throw new Error("Daemon is not connected.");
    const answer = this.answers[type];
    if (answer === undefined) throw new Error(`no answer for ${type}`);
    if (answer instanceof Error) throw answer;
    const frame = typeof answer === "function" ? await answer(body) : answer;
    return { type: options.reply, ...frame } as T;
  }

  on(type: string, listener: (frame: Frame) => void): () => void {
    const set = this.listeners.get(type) ?? new Set();
    set.add(listener);
    this.listeners.set(type, set);
    return () => set.delete(listener);
  }

  onState(listener: (state: ConnectionState) => void): () => void {
    this.stateListeners.add(listener);
    return () => this.stateListeners.delete(listener);
  }

  send(frame: Frame): void {
    this.sent.push(frame);
  }

  /** The computer sends `frame`. */
  push(frame: Frame): void {
    for (const listener of this.listeners.get(frame.type) ?? []) listener(frame);
  }

  setState(state: ConnectionState): void {
    this.state = state;
    for (const listener of this.stateListeners) listener(state);
    if (state === "connected") this.push({ type: "connected" });
  }

  count(type: string): number {
    return this.asked.filter((entry) => entry.type === type).length;
  }
}

export function testServer(overrides: Partial<StoredServer> = {}): StoredServer {
  return {
    id: "s1",
    name: "Desk",
    url: "ws://desk.test:9877/ws",
    daemonId: "a".repeat(64),
    daemonPublicKey: "b".repeat(64),
    ...overrides,
  } as StoredServer;
}
