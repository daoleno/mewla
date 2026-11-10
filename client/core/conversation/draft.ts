import type { KeyValueStore } from "../platform";

const DRAFT_PREFIX = "mewla.draft.v1:";
export const DRAFT_MAX_AGE_MS = 14 * 24 * 60 * 60 * 1000;
export const DRAFT_WRITE_DELAY_MS = 400;

/** The Expo app's chat cache key: its drafts and queued messages are found under it after the upgrade. */
export function chatCacheKey(serverId: string, workerId: string, conversationScopeKey?: string): string {
  return conversationScopeKey ? `${serverId}:scope:${conversationScopeKey}` : `${serverId}:agent:${workerId}`;
}

/**
 * What the person typed and has not sent, for one chat, kept as the Expo app
 * kept it (`mewla.draft.v1:<cache key>` → `{t, v}`). It depends only on the chat,
 * so it is there before the computer answers, and while Brain cannot start.
 */
export class Draft {
  private timer: ReturnType<typeof setTimeout> | null = null;
  private pending: (() => void) | null = null;

  constructor(
    private readonly storage: KeyValueStore,
    readonly cacheKey: string,
    private readonly now: () => number = Date.now,
  ) {}

  /** The saved draft, or "" (one older than two weeks is dropped). */
  async load(): Promise<string> {
    const key = DRAFT_PREFIX + this.cacheKey;
    try {
      const raw = await this.storage.getItem(key);
      if (!raw) return "";
      const parsed = JSON.parse(raw) as { t?: number; v?: string };
      if (typeof parsed.v !== "string" || typeof parsed.t !== "number" || this.now() - parsed.t > DRAFT_MAX_AGE_MS) {
        await this.storage.removeItem(key);
        return "";
      }
      return parsed.v;
    } catch {
      return "";
    }
  }

  /** Keeps the draft shortly after typing pauses; an empty one is removed at once. */
  save(value: string): void {
    this.cancel();
    const key = DRAFT_PREFIX + this.cacheKey;
    const write = () => {
      this.cancel();
      const done = value ? this.storage.setItem(key, JSON.stringify({ t: this.now(), v: value })) : this.storage.removeItem(key);
      // A draft is a convenience; storage being full or blocked is not an error.
      void done.catch(() => {});
    };
    if (!value) return write();
    this.pending = write;
    this.timer = setTimeout(write, DRAFT_WRITE_DELAY_MS);
  }

  /** Writes what is waiting now: the last keystrokes before a switch or "New chat" are kept. */
  flush(): void {
    this.pending?.();
  }

  private cancel(): void {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.pending = null;
  }
}
