import { filesToFetch, type CatManifest, type CatManifestFile } from "./catPark";

/** Fewer missing files than this are cheaper one by one than as the whole archive. */
export const ARCHIVE_MIN_FILES = 8;
export const FETCH_PARALLEL = 4;

export interface CatSyncProgress {
  done: number;
  total: number;
}

/** What a sync needs from the outside world; the App wires it to signed requests and the file system. */
export interface CatSyncDeps {
  manifest(): Promise<CatManifest>;
  /** The whole bundle as one tar, or null when this computer predates the archive. */
  archive(): Promise<Uint8Array | null>;
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
}

export class CatSyncTimeout extends Error {
  constructor() {
    super("Bringing the cat's house over took too long.");
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
 * Brings the phone's files up to the manifest: the whole archive in one request
 * when much is missing (every round trip costs a second or more over a tunnel),
 * the rest file by file. Every file is checked against its manifest digest
 * before it is written, and the sync as a whole always ends by its deadline.
 */
export async function runCatSync(deps: CatSyncDeps, options: CatSyncOptions): Promise<CatManifest> {
  const timers = options.timers ?? { set: (callback, ms) => setTimeout(callback, ms), clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>) };
  let expire: (error: Error) => void = () => undefined;
  const expired = new Promise<never>((_, reject) => {
    expire = reject;
  });
  expired.catch(() => undefined);
  let over = false;
  const timer = timers.set(() => {
    over = true;
    expire(new CatSyncTimeout());
  }, options.deadlineMs);

  const work = (async () => {
    const manifest = await deps.manifest();
    const missing = filesToFetch(manifest, deps.present(manifest));
    const total = missing.length;
    let done = 0;
    const settled = () => options.onProgress?.({ done: ++done, total });
    options.onProgress?.({ done, total });

    let pending = missing;
    if (pending.length >= ARCHIVE_MIN_FILES) {
      const packed = await deps.archive();
      if (packed) {
        const wanted = new Map(pending.map((file) => [file.path, file]));
        try {
          for (const entry of readTar(packed)) {
            if (over) break;
            const file = wanted.get(entry.path);
            if (!file || entry.data.byteLength !== file.size) continue;
            if ((await deps.sha256Hex(entry.data)) !== file.sha256) continue;
            deps.write(manifest, file, entry.data);
            wanted.delete(entry.path);
            settled();
          }
        } catch {
          // A damaged archive costs only what it did not deliver: fetched singly below.
        }
        pending = [...wanted.values()];
      }
    }

    let next = 0;
    const worker = async () => {
      while (next < pending.length && !over) {
        const file = pending[next++];
        const bytes = await deps.file(file);
        if (bytes.byteLength !== file.size) throw new Error(`The cat's ${file.path} arrived incomplete.`);
        if ((await deps.sha256Hex(bytes)) !== file.sha256) throw new Error(`The cat's ${file.path} did not match its digest.`);
        deps.write(manifest, file, bytes);
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
