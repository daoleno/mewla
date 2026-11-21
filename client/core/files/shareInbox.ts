import { MAX_COMPOSER_ATTACHMENTS, type UploadDocumentAsset } from "./uploads";

/**
 * What other apps hand to Mewla (Android's share sheet and Open with), held until Brain's composer can take it.
 * The host copies each share and keeps it until it is acknowledged (`share.take`, `share.ack`); this decides
 * when a taken share goes into the composer, so none is lost while the app is not ready:
 * - not paired yet, or loading, or the computer must be paired again: it waits;
 * - the computer is still connecting: it waits a moment (a chip that fails only because the socket was not up yet
 *   is no help), then goes in anyway, so an unreachable computer shows the failed chip with its Retry;
 * - the composer holds the most it takes: what fits goes in, the rest waits for room, and the person is told once.
 */

export type SharedBatch = {
  id: string;
  /** The shared text or link (a page's title above its link); empty when there is none. */
  text: string;
  /** Why nothing came of this share, when nothing did. */
  problem: string | null;
  assets: UploadDocumentAsset[];
};

/** Where a share lands: Brain's composer. */
export interface ShareTarget {
  /** How many more files its chips take now. */
  room(): number;
  /** Puts these in as chips (they upload at once); never more than `room()`. */
  addFiles(assets: UploadDocumentAsset[]): void;
  /** Adds text to the draft, below what is typed. */
  addText(text: string): void;
  /** Where attachment errors are shown. */
  notice(text: string): void;
}

export interface ShareConditions {
  /** The computers are loaded and one is current. */
  paired(): boolean;
  /** Brain's draft exists to take text (it needs the computer's Brain to have answered once). */
  draft(): boolean;
  /** The computer needs pairing again. */
  pairAgain(): boolean;
  connected(): boolean;
  now(): number;
}

export type ShareHost = {
  /** The shares not yet acknowledged. */
  take(): Promise<SharedBatch[]>;
  ack(ids: string[]): Promise<void>;
};

/** How long a computer that is still connecting is waited for. */
export const CONNECT_GRACE_MS = 6_000;

export class ShareInbox {
  private waiting: Array<{ batch: SharedBatch; textDone: boolean; told: boolean }> = [];
  private seen = new Set<string>();
  private target: ShareTarget | null = null;
  private connectingSince: number | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private host: ShareHost,
    private conditions: ShareConditions,
    /** The app comes to Brain's chat when a share arrives. */
    private land: () => void = () => {},
    private timers: { set: (run: () => void, ms: number) => ReturnType<typeof setTimeout>; clear: (timer: ReturnType<typeof setTimeout>) => void } = { set: setTimeout, clear: clearTimeout },
  ) {}

  /** Asks the host for what waits (at start, and when it says a share arrived). */
  async pull(): Promise<void> {
    let batches: SharedBatch[];
    try {
      batches = await this.host.take();
    } catch {
      return;
    }
    this.receive(batches);
  }

  receive(batches: readonly SharedBatch[]): void {
    const fresh = batches.filter((batch) => batch && typeof batch.id === "string" && !this.seen.has(batch.id));
    if (!fresh.length) return;
    fresh.forEach((batch) => {
      this.seen.add(batch.id);
      this.waiting.push({ batch: { ...batch, assets: Array.isArray(batch.assets) ? batch.assets : [], text: batch.text ?? "" }, textDone: false, told: false });
    });
    this.land();
    this.deliver();
  }

  /** Brain's composer is made (a target) or gone (null). */
  setTarget(target: ShareTarget | null): void {
    this.target = target;
    if (target) this.deliver();
  }

  /** Something it waits on may have changed (a computer, the connection, room in the composer). */
  deliver(): void {
    // Adding to the composer changes what this waits on, and that asks again from inside: it is one pass.
    if (this.delivering) {
      this.again = true;
      return;
    }
    this.delivering = true;
    try {
      do {
        this.again = false;
        this.pass();
      } while (this.again);
    } finally {
      this.delivering = false;
    }
  }

  private delivering = false;
  private again = false;

  private pass(): void {
    if (this.timer) {
      this.timers.clear(this.timer);
      this.timer = null;
    }
    const target = this.target;
    if (!target || !this.waiting.length || !this.conditions.paired() || this.conditions.pairAgain()) return;
    // Text goes in at once, once there is a draft for it: it needs no connection.
    for (const entry of this.waiting) {
      if (entry.textDone) continue;
      const { batch } = entry;
      if (batch.text.trim()) {
        if (!this.conditions.draft()) continue;
        target.addText(batch.text);
      } else if (batch.problem && !batch.assets.length) target.notice(batch.problem);
      entry.textDone = true;
    }
    if (this.waiting.some((entry) => entry.batch.assets.length) && !this.conditions.connected()) {
      const now = this.conditions.now();
      this.connectingSince ??= now;
      const left = CONNECT_GRACE_MS - (now - this.connectingSince);
      if (left > 0) {
        this.timer = this.timers.set(() => this.deliver(), left);
        return this.finish();
      }
    } else if (this.conditions.connected()) {
      this.connectingSince = null;
    }
    for (const entry of this.waiting) {
      const { batch } = entry;
      const room = Math.max(0, target.room());
      const take = batch.assets.slice(0, room);
      // Forgotten before it goes in: adding it changes what `deliver` waits on.
      batch.assets = batch.assets.slice(take.length);
      if (take.length) target.addFiles(take);
      if (batch.assets.length && !entry.told) {
        entry.told = true;
        target.notice(`Choose up to ${MAX_COMPOSER_ATTACHMENTS} files. Remove an attachment to make room.`);
      }
      // The rest of this share goes first: a later share does not pass it.
      if (batch.assets.length) break;
    }
    this.finish();
  }

  /** The shares that are whole in the composer are acknowledged and forgotten. */
  private finish(): void {
    const done = this.waiting.filter((entry) => entry.textDone && !entry.batch.assets.length).map((entry) => entry.batch.id);
    if (!done.length) return;
    this.waiting = this.waiting.filter((entry) => !done.includes(entry.batch.id));
    void this.host.ack(done).catch(() => {});
  }

  /** For tests and the UI: shares not yet in a composer. */
  pending(): number {
    return this.waiting.length;
  }
}
