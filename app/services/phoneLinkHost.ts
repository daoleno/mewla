import type { PhoneLinkOptions, PhoneLinkState } from "./phoneLink";

/** The one signed link from this phone to its computer, owned by the process rather than by a screen. */
export type PhoneLinkHandle = { start(): void; stop(): void };

export type PhoneLinkFactory = (options: PhoneLinkOptions) => PhoneLinkHandle;

export type PhoneLinkHostState = PhoneLinkState | "off";

/**
 * The link lives here, not in a React component: the Phone page, a headless start after a reboot
 * and a page that was swiped away all see the same one, so two of them can never trade the
 * computer's "replaced" close. A screen watches it; only the Labs switch or a server switch stops it.
 */
export class PhoneLinkHost {
  private active: { serverId: string; link: PhoneLinkHandle } | null = null;
  private state: PhoneLinkHostState = "off";
  private readonly watchers = new Set<(state: PhoneLinkHostState) => void>();

  constructor(private readonly create: PhoneLinkFactory) {}

  /** Keeps a link to this server up: the one already running when it is for the same server, else a new one. */
  hold(options: Omit<PhoneLinkOptions, "onState">) {
    if (this.active?.serverId === options.server.id) return;
    this.release();
    const link = this.create({ ...options, onState: (state) => this.set(state) });
    this.active = { serverId: options.server.id, link };
    link.start();
  }

  release() {
    const active = this.active;
    this.active = null;
    active?.link.stop();
    this.set("off");
  }

  get current(): PhoneLinkHostState {
    return this.state;
  }

  get serverId(): string | null {
    return this.active?.serverId ?? null;
  }

  /** Calls [listener] now and on every change; returns how to stop. */
  watch(listener: (state: PhoneLinkHostState) => void): () => void {
    this.watchers.add(listener);
    listener(this.state);
    return () => {
      this.watchers.delete(listener);
    };
  }

  private set(state: PhoneLinkHostState) {
    if (this.active === null && state !== "off") return; // a link that was released says nothing more
    if (this.state === state) return;
    this.state = state;
    this.watchers.forEach((watcher) => watcher(state));
  }
}

/**
 * Resolves once the link is up, or can never come up on its own (taken by another phone, refused as
 * too old, released), or after [maxMs]. A headless task awaits this: while it runs, React Native keeps
 * its timers going in the background, and the link's reconnect waits on them.
 */
export function untilLinkSettles(
  host: Pick<PhoneLinkHost, "watch">,
  maxMs: number,
  timers: { set(fn: () => void, ms: number): unknown; clear(handle: unknown): void } = {
    set: (fn, ms) => setTimeout(fn, ms),
    clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
  },
): Promise<PhoneLinkHostState> {
  return new Promise((resolve) => {
    let done = false;
    let stop: (() => void) | null = null;
    let cap: unknown = null;
    const finish = (state: PhoneLinkHostState) => {
      if (done) return;
      done = true;
      if (cap !== null) timers.clear(cap);
      stop?.();
      resolve(state);
    };
    stop = host.watch((state) => {
      if (state === "online" || state === "replaced" || state === "unsupported") finish(state);
    });
    if (done) {
      stop();
      return;
    }
    cap = timers.set(() => finish("offline"), maxMs);
  });
}
