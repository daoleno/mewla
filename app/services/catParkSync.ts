import { filesToFetch, type CatManifest, type CatManifestFile } from "./catPark";

/** Fewer missing files than this are cheaper one by one than as archive parts. */
export const ARCHIVE_MIN_FILES = 8;
/** The bundle is read in this many slices (about 330 KB each): small enough to survive a dropped connection. */
export const ARCHIVE_PARTS = 16;
/** Each request is tried this many times before its part or file counts as failed. */
export const REQUEST_ATTEMPTS = 3;
const RETRY_BACKOFF_MS = 400;
export const FETCH_PARALLEL = 4;

export interface CatSyncProgress {
  done: number;
  total: number;
}

/** What a sync needs from the outside world; the App wires it to signed requests and the file system. */
export interface CatSyncDeps {
  manifest(): Promise<CatManifest>;
  /** Slice `part` of `of` of the bundle as a tar (file i of the manifest is in slice i % of), or null when this computer predates the archive. */
  archive(part: number, of: number): Promise<Uint8Array | null>;
  file(file: CatManifestFile): Promise<Uint8Array>;
  sha256Hex(bytes: Uint8Array): Promise<string>;
  /** Path to size of what the phone already holds for this manifest's version. */
  present(manifest: CatManifest): Map<string, number>;
  write(manifest: CatManifest, file: CatManifestFile, bytes: Uint8Array): void;
}

export interface CatSyncOptions {
  /** The whole sync ends by then, whatever any request is doing. */
  deadlineMs: number;
  onProgress?: (progress: CatSyncProgress) => void;
  timers?: {
    set: (callback: () => void, ms: number) => unknown;
    clear: (handle: unknown) => void;
  };
  /** Waits between a failed request and its next try. */
  sleep?: (ms: number) => Promise<void>;
}

/** The computer answered, and said no: asking again will not change that unless it was a server error. */
export class CatRequestError extends Error {
  constructor(message: string, readonly retryable: boolean) {
    super(message);
  }
}

export type CatSyncStage = "manifest" | "part" | "file" | "digest" | "write" | "deadline";

/** Short and plain: what a person can read in one line, and a log can keep. */
export function catFailureCause(error: unknown): string {
  const text = (error instanceof Error ? error.message : String(error)).replace(/https?:\/\/\S+/g, "<url>").trim();
  if ((error instanceof Error && error.name === "AbortError") || /abort/i.test(text)) return "timed out";
  if (/network request failed|failed to fetch|network error|socket|reset|econn|connection (lost|closed|reset)/i.test(text)) return "connection dropped";
  return text.length > 80 ? `${text.slice(0, 77)}...` : text || "unknown error";
}

const STAGE_LABEL: Record<CatSyncStage, string> = {
  manifest: "The house's file list didn't arrive",
  part: "A part of the house didn't arrive",
  file: "A file of the house didn't arrive",
  digest: "A file of the house arrived damaged",
  write: "The house couldn't be saved on this phone",
  deadline: "Bringing the house over is taking too long",
};

/** A sync that ended without the whole bundle: which step, how far it got, and why. */
export class CatSyncError extends Error {
  readonly cause_: string;
  readonly refused: boolean;
  constructor(
    readonly stage: CatSyncStage,
    error: unknown,
    readonly done: number,
    readonly total: number,
    readonly trail: string[],
  ) {
    const refused = error instanceof CatRequestError && !error.retryable;
    const cause = catFailureCause(error);
    super(refused ? cause : `${STAGE_LABEL[stage]} (${cause}), ${done} of ${total} files so far.`);
    this.cause_ = cause;
    this.refused = refused;
  }

  /** One line for the daemon log. */
  diagnostic(): string {
    return `stage=${this.stage} done=${this.done}/${this.total} error="${this.cause_}" trail=${this.trail.join("; ")}`;
  }
}

export class CatSyncTimeout extends CatSyncError {
  constructor(done = 0, total = 0, trail: string[] = []) {
    super("deadline", new Error("slow link"), done, total, trail);
  }
}

const BLOCK = 512;

function asciiField(block: Uint8Array, start: number, length: number): string {
  let text = "";
  for (let i = start; i < start + length && block[i] !== 0; i++) text += String.fromCharCode(block[i]);
  return text;
}

/** The regular files of a ustar archive, in order. Throws on a damaged one. */
export function* readTar(bytes: Uint8Array): Generator<{ path: string; data: Uint8Array }> {
  let offset = 0;
  while (offset + BLOCK <= bytes.byteLength) {
    const header = bytes.subarray(offset, offset + BLOCK);
    if (header.every((byte) => byte === 0)) return;
    const size = Number.parseInt(asciiField(header, 124, 12).trim(), 8);
    if (!Number.isInteger(size) || size < 0) throw new Error("The cat's archive is damaged.");
    const start = offset + BLOCK;
    if (start + size > bytes.byteLength) throw new Error("The cat's archive ended early.");
    const kind = header[156];
    if (kind === 0 || kind === 0x30) {
      const prefix = asciiField(header, 345, 155);
      const name = asciiField(header, 0, 100);
      yield { path: prefix ? `${prefix}/${name}` : name, data: bytes.subarray(start, start + size) };
    }
    offset = start + Math.ceil(size / BLOCK) * BLOCK;
  }
  throw new Error("The cat's archive ended early.");
}

/**
 * Brings the phone's files up to the manifest: archive parts, a few requests
 * and a few at a time, when much is missing (every round trip costs a second or
 * more over a tunnel), the rest file by file. Every request is tried a few
 * times, a part that still fails is fetched file by file instead (small
 * requests survive a flaky link), and every file is checked against its
 * manifest digest before it is written. The sync as a whole always ends by its
 * deadline, and a failure says which step it was and how far it got.
 */
export async function runCatSync(deps: CatSyncDeps, options: CatSyncOptions): Promise<CatManifest> {
  const timers = options.timers ?? { set: (callback, ms) => setTimeout(callback, ms), clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>) };
  const sleep = options.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
  let done = 0;
  let total = 0;
  const trail: string[] = [];
  const note = (line: string) => {
    if (trail.length < 8) trail.push(line);
  };
  const fail = (stage: CatSyncStage, error: unknown) => (error instanceof CatSyncError ? error : new CatSyncError(stage, error, done, total, trail));

  let expire: (error: Error) => void = () => undefined;
  const expired = new Promise<never>((_, reject) => {
    expire = reject;
  });
  expired.catch(() => undefined);
  let over = false;
  const timer = timers.set(() => {
    over = true;
    expire(new CatSyncTimeout(done, total, trail));
  }, options.deadlineMs);

  /** One request, tried again after a pause unless the computer refused it. */
  async function attempt<T>(label: string, run: () => Promise<T>): Promise<T> {
    for (let tries = 1; ; tries++) {
      try {
        return await run();
      } catch (error) {
        const refused = error instanceof CatRequestError && !error.retryable;
        if (over || refused || tries >= REQUEST_ATTEMPTS) {
          note(`${label} x${tries}: ${catFailureCause(error)}`);
          throw error;
        }
        await sleep(RETRY_BACKOFF_MS * tries);
      }
    }
  }

  const work = (async () => {
    let manifest: CatManifest;
    try {
      manifest = await attempt("manifest", () => deps.manifest());
    } catch (error) {
      throw fail("manifest", error);
    }
    const missing = filesToFetch(manifest, deps.present(manifest));
    total = missing.length;
    const settled = () => {
      done++;
      options.onProgress?.({ done, total });
    };
    options.onProgress?.({ done, total });
    const store = (file: CatManifestFile, bytes: Uint8Array) => {
      try {
        deps.write(manifest, file, bytes);
      } catch (error) {
        throw fail("write", error);
      }
    };

    let pending = missing;
    if (pending.length >= ARCHIVE_MIN_FILES) {
      const wanted = new Map(pending.map((file) => [file.path, file]));
      const position = new Map(manifest.files.map((file, index) => [file.path, index]));
      const parts = [...new Set(pending.map((file) => position.get(file.path)! % ARCHIVE_PARTS))];
      let nextPart = 0;
      let unsupported = false;
      let delivered = 0;
      let lost = 0;
      const partWorker = async () => {
        // Parts keep coming while some arrive; a link that has lost three in a row with none through is not worth more.
        while (nextPart < parts.length && !over && !unsupported && !(lost >= 3 && delivered === 0)) {
          const part = parts[nextPart++];
          let packed: Uint8Array | null;
          try {
            packed = await attempt(`part ${part}/${ARCHIVE_PARTS}`, () => deps.archive(part, ARCHIVE_PARTS));
          } catch (error) {
            if (error instanceof CatRequestError && !error.retryable) throw fail("part", error);
            // What other parts bring stays written, and this part's files are fetched singly below.
            lost++;
            continue;
          }
          if (!packed) {
            unsupported = true;
            return;
          }
          delivered++;
          try {
            for (const entry of readTar(packed)) {
              if (over) break;
              const file = wanted.get(entry.path);
              if (!file || entry.data.byteLength !== file.size) continue;
              if ((await deps.sha256Hex(entry.data)) !== file.sha256) continue;
              store(file, entry.data);
              wanted.delete(entry.path);
              settled();
            }
          } catch (error) {
            if (error instanceof CatSyncError) throw error;
            // A damaged part costs only what it did not deliver: fetched singly below.
          }
        }
      };
      await Promise.all(Array.from({ length: Math.min(FETCH_PARALLEL, parts.length) }, partWorker));
      pending = [...wanted.values()];
    }

    let next = 0;
    const worker = async () => {
      while (next < pending.length && !over) {
        const file = pending[next++];
        let bytes: Uint8Array;
        try {
          bytes = await attempt(file.path, async () => {
            const got = await deps.file(file);
            if (got.byteLength !== file.size) throw new CatSyncError("file", new Error("arrived incomplete"), done, total, trail);
            if ((await deps.sha256Hex(got)) !== file.sha256) throw new CatSyncError("digest", new Error("did not match its digest"), done, total, trail);
            return got;
          });
        } catch (error) {
          throw fail("file", error);
        }
        store(file, bytes);
        settled();
      }
    };
    await Promise.all(Array.from({ length: Math.min(FETCH_PARALLEL, pending.length) }, worker));
    return manifest;
  })();
  work.catch(() => undefined);

  try {
    return await Promise.race([work, expired]);
  } finally {
    timers.clear(timer);
  }
}
