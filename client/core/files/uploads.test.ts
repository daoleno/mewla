import { beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import {
  checkPickedCount,
  createAttachmentUploadOperation,
  pickLimit,
  V1_MAX_UPLOAD_FILE_BYTES,
  type NativeUploadProgress,
  type UploadDocumentAsset,
  type UploadHost,
} from "./uploads";

const server = { id: "s1", name: "Desk", url: "wss://mewla.test/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const asset = (overrides: Partial<UploadDocumentAsset> = {}): UploadDocumentAsset => ({ uri: "content://fixture/document/opaque", name: "../../报告 2026.ZIP", mimeType: "application/zip", size: 1024, ...overrides });
const done = { status: 200, body: JSON.stringify({ path: "/state/uploads/server-file.zip", name: "../../报告 2026.ZIP" }) };

function fakeHost(answer: () => Promise<{ status: number; body: string }> = async () => done) {
  const calls: Array<Parameters<UploadHost["upload"]>[0]> = [];
  const cancelled: string[] = [];
  const listeners = new Map<string, (progress: NativeUploadProgress) => void>();
  let cancelError: Error | null = null;
  const host: UploadHost = {
    upload: (request) => {
      calls.push(request);
      return answer();
    },
    cancel: (id) => {
      cancelled.push(id);
      if (cancelError) throw cancelError;
    },
    onProgress: (id, listener) => {
      listeners.set(id, listener);
      return () => listeners.delete(id);
    },
  };
  return {
    host,
    calls,
    cancelled,
    listeners,
    progress: (bytesSent: number, totalBytes: number) => listeners.get(calls[0]!.uploadId)?.({ bytesSent, totalBytes }),
    failCancel: (error: Error) => (cancelError = error),
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => (resolve = next));
  return { promise, resolve };
}

async function flush() {
  for (let index = 0; index < 20; index += 1) await Promise.resolve();
}

beforeEach(() => {
  installMemoryPlatform();
});

describe("attachment upload through the host", () => {
  test("streams the picked file's own URI to /upload, signed for upload, with its type and encoded name", async () => {
    const h = fakeHost();
    const attachment = await createAttachmentUploadOperation(asset(), server, h.host).result;
    expect(h.calls).toHaveLength(1);
    expect(h.calls[0]).toMatchObject({
      url: "https://mewla.test/upload",
      fileUri: "content://fixture/document/opaque",
      expectedSize: 1024,
      headers: { "Content-Type": "application/zip", "X-Mewla-Upload-Name": "..%2F..%2F%E6%8A%A5%E5%91%8A%202026.ZIP" },
    });
    expect(h.calls[0]!.headers.Authorization).toMatch(/v1:/);
    expect(attachment).toEqual({ name: "../../报告 2026.ZIP", path: "/state/uploads/server-file.zip", localUri: "content://fixture/document/opaque", mimeType: "application/zip" });
    expect(h.listeners.size).toBe(0);
  });

  test("a failed transfer stops listening; a non-2xx answer says the computer's own words", async () => {
    const failed = fakeHost(async () => {
      throw new Error("selected source became unreadable");
    });
    await expect(createAttachmentUploadOperation(asset(), server, failed.host).result).rejects.toThrow("selected source became unreadable");
    expect(failed.listeners.size).toBe(0);
    const full = fakeHost(async () => ({ status: 507, body: "upload storage capacity reached\n" }));
    await expect(createAttachmentUploadOperation(asset(), server, full.host).result).rejects.toThrow("upload storage capacity reached");
  });

  test("nothing starts for a file above the 2 GiB limit or a name too long to send", () => {
    const h = fakeHost();
    expect(() => createAttachmentUploadOperation(asset({ size: V1_MAX_UPLOAD_FILE_BYTES + 1 }), server, h.host)).toThrow("File exceeds the 2 GiB upload limit.");
    expect(() => createAttachmentUploadOperation(asset({ name: "a".repeat(1025) }), server, h.host)).toThrow("File name is too long.");
    expect(h.calls).toHaveLength(0);
  });

  test("an unknown size stays unknown", async () => {
    const h = fakeHost();
    await createAttachmentUploadOperation(asset({ size: null }), server, h.host).result;
    expect(h.calls[0]!.expectedSize).toBeNull();
  });

  test("progress only moves forward, for known and unknown totals", async () => {
    const answer = deferred<{ status: number; body: string }>();
    const h = fakeHost(() => answer.promise);
    const snapshots: unknown[] = [];
    const operation = createAttachmentUploadOperation(asset(), server, h.host, { onProgress: (p) => snapshots.push(p), now: () => 0 });
    await flush();
    h.progress(128, -1);
    h.progress(640, 1024);
    h.progress(512, 1024);
    expect(snapshots).toEqual([
      { transferredBytes: 128, totalBytes: null, fraction: null },
      { transferredBytes: 640, totalBytes: 1024, fraction: 0.625 },
      { transferredBytes: 640, totalBytes: 1024, fraction: 0.625 },
    ]);
    answer.resolve(done);
    await operation.result;
  });

  test("speed and time left show after a stable sample window", async () => {
    const answer = deferred<{ status: number; body: string }>();
    const h = fakeHost(() => answer.promise);
    const snapshots: unknown[] = [];
    let now = 1_000;
    const operation = createAttachmentUploadOperation(asset(), server, h.host, { now: () => now, onProgress: (p) => snapshots.push(p) });
    await flush();
    now = 1_100;
    h.progress(128, 1024);
    now = 1_500;
    h.progress(512, 1024);
    expect(snapshots).toEqual([
      { transferredBytes: 128, totalBytes: 1024, fraction: 0.125 },
      { transferredBytes: 512, totalBytes: 1024, fraction: 0.5, bytesPerSecond: 1024, etaSeconds: 1 },
    ]);
    answer.resolve(done);
    await operation.result;
  });

  test("cancel stops the exact upload once and ignores what comes after", async () => {
    const answer = deferred<{ status: number; body: string }>();
    const h = fakeHost(() => answer.promise);
    const snapshots: unknown[] = [];
    const operation = createAttachmentUploadOperation(asset(), server, h.host, { onProgress: (p) => snapshots.push(p), now: () => 0 });
    await flush();
    h.progress(128, 1024);
    expect(operation.cancel()).toBeNull();
    expect(operation.cancel()).toBeNull();
    h.progress(1024, 1024);
    answer.resolve(done);
    await expect(operation.result).rejects.toThrow("Attachment upload cancelled.");
    expect(snapshots).toEqual([{ transferredBytes: 128, totalBytes: 1024, fraction: 0.125 }]);
    expect(h.cancelled).toEqual([h.calls[0]!.uploadId]);
  });

  test("a cancel that itself fails says so, and is not tried twice", async () => {
    const answer = deferred<{ status: number; body: string }>();
    const h = fakeHost(() => answer.promise);
    h.failCancel(new Error("native cancellation failed"));
    const operation = createAttachmentUploadOperation(asset(), server, h.host);
    await flush();
    expect(operation.cancel()?.message).toBe("native cancellation failed");
    expect(operation.cancel()).toBeNull();
    expect(h.cancelled).toHaveLength(1);
    answer.resolve(done);
    await expect(operation.result).rejects.toThrow("native cancellation failed");
  });
});

describe("document selection", () => {
  test("more files than there is room for are refused before anything uploads", () => {
    const three = ["one", "two", "three"].map((name) => asset({ uri: `content://fixture/${name}`, name }));
    expect(checkPickedCount(three, 3).map((a) => a.uri)).toEqual(three.map((a) => a.uri));
    expect(() => checkPickedCount(three, 2)).toThrow("Choose up to 2 files. Remove an attachment to make room.");
    expect(() => pickLimit(0)).toThrow("Remove an attachment before selecting more files.");
    expect(pickLimit(8)).toBe(8);
  });
});
