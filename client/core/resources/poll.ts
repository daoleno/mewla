import type { Daemon } from "../more/daemon";
import { normalizeResourceTelemetry, type ResourceTelemetry } from "./resourceTelemetry";

export const RESOURCE_POLL_MS = 5_000;

export interface ResourceReading {
  telemetry: ResourceTelemetry | null;
  error: string;
  reading: boolean;
}

export interface Timers {
  setTimeout(run: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
}

const systemTimers: Timers = {
  setTimeout: (run, ms) => setTimeout(run, ms),
  clearTimeout: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

/** One reading of the computer's resources, in the page's words when it fails. */
export async function readResources(daemon: Daemon, signal?: AbortSignal): Promise<ResourceTelemetry> {
  const frame = await daemon
    .request("get_resource_telemetry", {}, { reply: "resource_telemetry", timeoutMs: 10_000, signal })
    .catch((error: unknown) => {
      const message = error instanceof Error ? error.message : "";
      if (/timed out/.test(message)) throw new Error("Resource telemetry timed out.");
      if (/cancelled/.test(message)) throw new Error("Resource telemetry cancelled.");
      throw new Error(message || "Resource telemetry failed.");
    });
  const telemetry = normalizeResourceTelemetry(frame);
  if (!telemetry) throw new Error("Invalid resource telemetry.");
  return telemetry;
}

/**
 * Reads the computer's resources while the page is in use: at once, then five seconds
 * after each answer, one request at a time. `stop` cancels the request in flight. A
 * failure keeps the last reading and says why; an answer older than the one held is ignored.
 */
export class ResourcePoller {
  private state: ResourceReading = { telemetry: null, error: "", reading: false };
  private timer: unknown = null;
  private abort: AbortController | null = null;
  private running = false;

  constructor(
    private readonly daemon: Daemon,
    private readonly onChange: (state: ResourceReading) => void,
    private readonly timers: Timers = systemTimers,
    held: ResourceTelemetry | null = null,
  ) {
    this.state = { telemetry: held, error: "", reading: false };
  }

  get current(): ResourceReading {
    return this.state;
  }

  start(): void {
    if (this.running) return;
    this.running = true;
    this.read();
  }

  stop(): void {
    this.running = false;
    if (this.timer !== null) this.timers.clearTimeout(this.timer);
    this.timer = null;
    this.abort?.abort();
    this.abort = null;
    if (this.state.reading) this.set({ reading: false });
  }

  /** Read now (Refresh, Try again), unless a read is under way. */
  refresh(): void {
    if (!this.running || this.abort) return;
    if (this.timer !== null) this.timers.clearTimeout(this.timer);
    this.timer = null;
    this.read();
  }

  private read(): void {
    const abort = new AbortController();
    this.abort = abort;
    this.set({ reading: true });
    readResources(this.daemon, abort.signal)
      .then((telemetry) => {
        if (this.abort !== abort) return;
        const held = this.state.telemetry;
        this.set({ telemetry: held && held.sampledAt > telemetry.sampledAt ? held : telemetry, error: "" });
      })
      .catch((error: unknown) => {
        if (this.abort !== abort) return;
        this.set({ error: error instanceof Error ? error.message : "Resource telemetry failed." });
      })
      .finally(() => {
        if (this.abort !== abort) return;
        this.abort = null;
        this.set({ reading: false });
        if (this.running) this.timer = this.timers.setTimeout(() => {
          this.timer = null;
          if (this.running) this.read();
        }, RESOURCE_POLL_MS);
      });
  }

  private set(change: Partial<ResourceReading>): void {
    this.state = { ...this.state, ...change };
    this.onChange(this.state);
  }
}
