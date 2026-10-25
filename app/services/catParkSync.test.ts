import { createHash } from "node:crypto";
import { describe, expect, test } from "bun:test";
import type { CatManifest, CatManifestFile } from "./catPark";
import {
  ARCHIVE_MIN_FILES,
  ARCHIVE_PARTS,
  REQUEST_ATTEMPTS,
  CatRequestError,
  CatSyncError,
  CatSyncTimeout,
  catFailureCause,
  readTar,
  runCatSync,
  type CatSyncDeps,
  type CatSyncProgress,
} from "./catParkSync";

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

const options = (extra: Record<string, unknown> = {}) => ({ deadlineMs: 120_000, sleep: async () => undefined, ...extra });

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
    expect([...calls.parts].sort((a, b) => a - b)).toEqual(Array.from({ length: ARCHIVE_PARTS }, (_, part) => part));
    expect(calls.files).toEqual([]);
    expect(written.size).toBe(manifest.files.length);
    expect(progress.length).toBeGreaterThan(ARCHIVE_PARTS);
    expect(progress.at(-1)).toEqual({ done: 123, total: 123 });
  });

  test("a request that fails once is asked again, and nothing is fetched singly", async () => {
    let failures = 0;
    const { deps, calls, written } = harness(40, {}, (entries, part) => {
      if (part === 2 && failures++ < REQUEST_ATTEMPTS - 1) throw new Error("Network request failed");
      return tarOf(entries);
    });
    await runCatSync(deps, options());
    expect(calls.parts.filter((part) => part === 2).length).toBe(REQUEST_ATTEMPTS);
    expect(calls.files).toEqual([]);
    expect(written.size).toBe(40);
  });

  test("a part that keeps failing is fetched file by file, small requests surviving where the big one did not", async () => {
    const { deps, calls, written } = harness(40, {}, (entries, part) => {
      if (part === 3) throw new Error("Network request failed");
      return tarOf(entries);
    });
    await runCatSync(deps, options());
    expect(calls.files.sort()).toEqual(["a/pic19.webp", "a/pic3.webp", "a/pic35.webp"]);
    expect(written.size).toBe(40);
  });

  test("a link that lost parts and can fetch nothing ends with the step and how far it got", async () => {
    const written = new Map<string, Uint8Array>();
    const failing = harness(
      40,
      { file: async () => Promise.reject(new Error("Network request failed")) },
      (entries, part) => {
        if (part === 3) throw new Error("Network request failed");
        return tarOf(entries);
      },
      written,
    );
    const error = await runCatSync(failing.deps, options()).then(
      () => null,
      (caught: unknown) => caught,
    );
    expect(error).toBeInstanceOf(CatSyncError);
    const failure = error as CatSyncError;
    expect(failure.stage).toBe("file");
    expect([failure.done, failure.total]).toEqual([37, 40]);
    expect(failure.message).toContain("connection dropped");
    expect(failure.diagnostic()).toMatch(/^stage=file done=37\/40 error="connection dropped" trail=/);
    expect(failure.trail.join(";")).toContain("part 3/16 x3");

    // A later run asks only for what is still missing.
    const retry = harness(40, {}, undefined, written);
    await runCatSync(retry.deps, options());
    expect(retry.calls.parts).toEqual([]);
    expect(retry.calls.files.sort()).toEqual(["a/pic19.webp", "a/pic3.webp", "a/pic35.webp"]);
    expect(written.size).toBe(40);
  });

  test("a dead link is not asked for every part", async () => {
    const { deps, calls } = harness(
      80,
      { file: async () => Promise.reject(new Error("Network request failed")) },
      () => {
        throw new Error("Network request failed");
      },
    );
    await expect(runCatSync(deps, options())).rejects.toBeInstanceOf(CatSyncError);
    expect(calls.parts.length).toBeLessThan(ARCHIVE_PARTS * REQUEST_ATTEMPTS);
  });

  test("a computer that says no is not asked again, and its answer is the reason", async () => {
    const { deps, calls } = harness(3, { file: async (file) => (calls.files.push(file.path), Promise.reject(new CatRequestError("manjaro runs an older Mewla; update it first", false))) });
    const error = (await runCatSync(deps, options()).catch((caught: unknown) => caught)) as CatSyncError;
    expect(error.refused).toBe(true);
    expect(error.message).toBe("manjaro runs an older Mewla; update it first");
    expect(calls.files.length).toBeLessThanOrEqual(3);
  });

  test("a server error is asked again", async () => {
    let asked = 0;
    const { deps } = harness(3, {
      manifest: async () => {
        if (asked++ === 0) throw new CatRequestError("bad gateway", true);
        return bundle(3).manifest;
      },
    });
    await runCatSync(deps, options());
    expect(asked).toBe(2);
  });

  test("a manifest that never arrives is named as the step", async () => {
    const { deps } = harness(3, { manifest: async () => Promise.reject(new Error("The operation was aborted")) });
    const error = (await runCatSync(deps, options()).catch((caught: unknown) => caught)) as CatSyncError;
    expect(error.stage).toBe("manifest");
    expect(error.cause_).toBe("timed out");
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

describe("failure causes", () => {
  test("are one short plain line and carry no address", () => {
    expect(catFailureCause(new Error("Network request failed"))).toBe("connection dropped");
    expect(catFailureCause(Object.assign(new Error("Aborted"), { name: "AbortError" }))).toBe("timed out");
    expect(catFailureCause(new Error("GET https://example.test/cat/x failed badly"))).toBe("GET <url> failed badly");
    expect(catFailureCause(new Error("x".repeat(200))).length).toBeLessThanOrEqual(80);
  });
});
