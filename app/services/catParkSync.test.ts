import { createHash } from "node:crypto";
import { describe, expect, test } from "bun:test";
import type { CatManifest, CatManifestFile } from "./catPark";
import { ARCHIVE_MIN_FILES, ARCHIVE_PARTS, CatSyncTimeout, readTar, runCatSync, type CatSyncDeps, type CatSyncProgress } from "./catParkSync";

const sha = (bytes: Uint8Array) => createHash("sha256").update(bytes).digest("hex");
const text = (value: string) => new TextEncoder().encode(value);

function tarOf(entries: { path: string; data: Uint8Array }[]): Uint8Array {
  const blocks: number[] = [];
  const put = (value: string, at: number, header: Uint8Array) => header.set(text(value), at);
  for (const { path, data } of entries) {
    const header = new Uint8Array(512);
    put(path, 0, header);
    put("0000644\0", 100, header);
    put(`${data.byteLength.toString(8).padStart(11, "0")}\0`, 124, header);
    header[156] = 0x30;
    put("ustar\0", 257, header);
    put("00", 263, header);
    header.fill(0x20, 148, 156);
    const sum = header.reduce((total, byte) => total + byte, 0);
    put(`${sum.toString(8).padStart(6, "0")}\0 `, 148, header);
    blocks.push(...header, ...data, ...new Uint8Array((512 - (data.byteLength % 512)) % 512));
  }
  blocks.push(...new Uint8Array(1024));
  return Uint8Array.from(blocks);
}

function bundle(count: number) {
  const contents = new Map<string, Uint8Array>();
  const files: CatManifestFile[] = [];
  for (let i = 0; i < count; i++) {
    const path = i === 0 ? "index.html" : `a/pic${i}.webp`;
    const data = text(`file ${i} ${"x".repeat(i * 37)}`);
    contents.set(path, data);
    files.push({ path, size: data.byteLength, sha256: sha(data) });
  }
  const manifest: CatManifest = { version: "0123456789abcdef", files };
  return { manifest, contents };
}

interface Harness {
  deps: CatSyncDeps;
  calls: { parts: number[]; files: string[] };
  written: Map<string, Uint8Array>;
}

/** What the daemon serves for one part: the manifest's files whose position is part modulo of. */
function partOf(contents: Map<string, Uint8Array>, part: number, of: number) {
  return [...contents].filter((_, index) => index % of === part).map(([path, data]) => ({ path, data }));
}

function harness(
  count: number,
  override: Partial<CatSyncDeps> = {},
  archivePart?: (entries: { path: string; data: Uint8Array }[], part: number) => Uint8Array | null,
  written = new Map<string, Uint8Array>(),
): Harness {
  const { manifest, contents } = bundle(count);
  const calls = { parts: [] as number[], files: [] as string[] };
  const deps: CatSyncDeps = {
    manifest: async () => manifest,
    archive: async (part, of) => {
      calls.parts.push(part);
      const entries = partOf(contents, part, of);
      return archivePart ? archivePart(entries, part) : tarOf(entries);
    },
    file: async (file) => {
      calls.files.push(file.path);
      return contents.get(file.path)!;
    },
    sha256Hex: async (bytes) => sha(bytes),
    present: () => new Map([...written].map(([path, data]) => [path, data.byteLength])),
    write: (_manifest, file, bytes) => void written.set(file.path, bytes),
    ...override,
  };
  return { deps, calls, written };
}

/** A clock the test fires by hand. */
function manualTimers() {
  let fire: () => void = () => undefined;
  let cleared = false;
  return {
    timers: { set: (callback: () => void) => ((fire = callback), 1), clear: () => void (cleared = true) },
    expire: () => fire(),
    wasCleared: () => cleared,
  };
}

const options = (extra: Record<string, unknown> = {}) => ({ deadlineMs: 120_000, ...extra });

describe("readTar", () => {
  test("yields each file with its bytes", () => {
    const entries = [{ path: "index.html", data: text("<html>") }, { path: "a/cat.webp", data: new Uint8Array(1500).fill(7) }, { path: "empty.js", data: new Uint8Array(0) }];
    const read = [...readTar(tarOf(entries))];
    expect(read.map((entry) => entry.path)).toEqual(["index.html", "a/cat.webp", "empty.js"]);
    expect(read.map((entry) => entry.data.byteLength)).toEqual([6, 1500, 0]);
    expect(read[1].data.every((byte) => byte === 7)).toBe(true);
  });

  test("refuses an archive cut short", () => {
    const whole = tarOf([{ path: "a", data: new Uint8Array(2000) }]);
    expect(() => [...readTar(whole.subarray(0, 1200))]).toThrow();
  });
});

describe("first open", () => {
  test("a bundle with much missing arrives as a few parts, never file by file, every file checked", async () => {
    const { deps, calls, written } = harness(123);
    const progress: CatSyncProgress[] = [];
    const manifest = await runCatSync(deps, options({ onProgress: (next: CatSyncProgress) => progress.push(next) }));
    expect([...calls.parts].sort()).toEqual(Array.from({ length: ARCHIVE_PARTS }, (_, part) => part));
    expect(calls.files).toEqual([]);
    expect(written.size).toBe(manifest.files.length);
    expect(progress.length).toBeGreaterThan(ARCHIVE_PARTS);
    expect(progress.at(-1)).toEqual({ done: 123, total: 123 });
  });

  test("a part that fails keeps what the others brought, and a retry asks only for what is missing", async () => {
    const written = new Map<string, Uint8Array>();
    const failing = harness(40, {}, (entries, part) => {
      if (part === 3) throw new Error("connection dropped");
      return tarOf(entries);
    }, written);
    await expect(runCatSync(failing.deps, options())).rejects.toThrow("connection dropped");
    expect(written.size).toBeGreaterThan(0);
    expect(written.size).toBeLessThan(40);

    const retry = harness(40, {}, undefined, written);
    await runCatSync(retry.deps, options());
    expect(retry.calls.parts).toContain(3);
    for (const landed of failing.calls.parts.filter((part) => part !== 3 && part < 3)) expect(retry.calls.parts).not.toContain(landed);
    expect(retry.calls.parts.length).toBeLessThan(ARCHIVE_PARTS);
    expect(retry.calls.files).toEqual([]);
    expect(written.size).toBe(40);
  });

  test("an entry that does not match its digest is fetched on its own", async () => {
    const { deps, calls, written } = harness(12, {}, (entries) =>
      tarOf(entries.map((entry) => (entry.path === "a/pic3.webp" ? { path: entry.path, data: text("tampered") } : entry))),
    );
    await runCatSync(deps, options());
    expect(calls.parts.length).toBeGreaterThan(0);
    expect(calls.files).toEqual(["a/pic3.webp"]);
    expect(written.size).toBe(12);
  });

  test("a computer without the archive is read file by file", async () => {
    const { deps, calls, written } = harness(20, { archive: async () => null });
    await runCatSync(deps, options());
    expect(calls.files.length).toBe(20);
    expect(written.size).toBe(20);
  });

  test("a damaged archive costs only what it did not deliver", async () => {
    const { deps, calls, written } = harness(20, {}, (entries) => tarOf(entries).subarray(0, 700));
    await runCatSync(deps, options());
    expect(calls.files.length).toBeGreaterThan(0);
    expect(calls.files.length).toBeLessThan(20);
    expect(written.size).toBe(20);
  });

  test("a few missing files are not worth the whole archive", async () => {
    const { deps, calls } = harness(ARCHIVE_MIN_FILES - 1);
    await runCatSync(deps, options());
    expect(calls.parts).toEqual([]);
    expect(calls.files.length).toBe(ARCHIVE_MIN_FILES - 1);
  });

  test("a file that arrives wrong ends the sync with a reason", async () => {
    const { deps } = harness(3, { file: async () => text("not the file") });
    await expect(runCatSync(deps, options())).rejects.toThrow(/incomplete|digest/);
  });
});

describe("a stalled computer", () => {
  test("the whole sync ends at its deadline, whichever request hangs", async () => {
    for (const hang of ["manifest", "archive", "file"] as const) {
      const never = () => new Promise<never>(() => undefined);
      const { deps } = harness(hang === "file" ? 3 : 40, { [hang]: never } as Partial<CatSyncDeps>);
      const clock = manualTimers();
      const outcome = runCatSync(deps, options({ timers: clock.timers })).then(
        () => "finished",
        (error: unknown) => (error instanceof CatSyncTimeout ? "timed out" : "failed"),
      );
      await Promise.resolve();
      clock.expire();
      expect(await outcome).toBe("timed out");
      expect(clock.wasCleared()).toBe(true);
    }
  });
});
