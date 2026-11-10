import { beforeEach, describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import { AttachmentUploadCancelledError, uploadFile } from "./upload";
import type { NativeUploadProgress, UploadHost } from "./uploads";

const server = { id: "s1", name: "Desk", url: "wss://mewla.test/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const file = { uri: "content://fixture/document/1", name: "notes.pdf", mimeType: "application/pdf", size: 4096 };

function fakeHost() {
  const calls: Array<Parameters<UploadHost["upload"]>[0]> = [];
  const cancelled: string[] = [];
  const listeners = new Map<string, (progress: NativeUploadProgress) => void>();
  let answer!: (value: { status: number; body: string }) => void;
  let refuse!: (error: Error) => void;
  const host: UploadHost = {
    upload: (request) => {
      calls.push(request);
      return new Promise((resolve, reject) => {
        answer = resolve;
        refuse = reject;
      });
    },
    cancel: (id) => {
      cancelled.push(id);
      refuse(new Error("Attachment upload cancelled."));
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
    progress: (bytesSent: number, totalBytes: number) => listeners.get(calls[0]!.uploadId)?.({ bytesSent, totalBytes }),
    answer: (status: number, body: unknown) => answer({ status, body: typeof body === "string" ? body : JSON.stringify(body) }),
  };
}

async function started(fake: ReturnType<typeof fakeHost>) {
  for (let i = 0; i < 50 && fake.calls.length === 0; i += 1) await Promise.resolve();
  expect(fake.calls).toHaveLength(1);
}

beforeEach(() => {
  installMemoryPlatform();
});

describe("uploadFile", () => {
  test("streams the picked file to the computer's /upload, signed, and answers where the computer keeps it", async () => {
    const fake = fakeHost();
    const seen: Array<[number, number | null]> = [];
    const result = uploadFile(fake.host, server, file, { onProgress: (sent, total) => seen.push([sent, total]) });
    await started(fake);
    const request = fake.calls[0]!;
    expect(request.url).toBe("https://mewla.test/upload");
    expect(request.fileUri).toBe(file.uri);
    expect(request.expectedSize).toBe(4096);
    expect(request.headers.Authorization).toBeTruthy();
    expect(request.headers["Content-Type"]).toBe("application/pdf");
    fake.progress(1024, 4096);
    fake.progress(4096, 4096);
    fake.answer(200, { path: "/state/uploads/notes.pdf", name: "notes.pdf" });
    expect(await result).toEqual({ name: "notes.pdf", path: "/state/uploads/notes.pdf", content_type: "application/pdf" });
    expect(seen).toEqual([[1024, 4096], [4096, 4096]]);
  });

  test("a total the host does not know is null, and a file without a type has no content_type", async () => {
    const fake = fakeHost();
    const seen: Array<[number, number | null]> = [];
    const result = uploadFile(fake.host, server, { uri: "content://x/2", name: "blob" }, { onProgress: (sent, total) => seen.push([sent, total]) });
    await started(fake);
    expect(fake.calls[0]!.headers["Content-Type"]).toBe("application/octet-stream");
    fake.progress(10, -1);
    fake.answer(200, { path: "/state/uploads/blob" });
    expect(await result).toEqual({ name: "blob", path: "/state/uploads/blob" });
    expect(seen).toEqual([[10, null]]);
  });

  test("aborting the signal cancels the host's upload and rejects as cancelled", async () => {
    const fake = fakeHost();
    const controller = new AbortController();
    const result = uploadFile(fake.host, server, file, { signal: controller.signal });
    await started(fake);
    controller.abort();
    await expect(result).rejects.toBeInstanceOf(AttachmentUploadCancelledError);
    expect(fake.cancelled).toEqual([fake.calls[0]!.uploadId]);
  });

  test("an already aborted signal never starts an upload", async () => {
    const fake = fakeHost();
    const controller = new AbortController();
    controller.abort();
    await expect(uploadFile(fake.host, server, file, { signal: controller.signal })).rejects.toBeInstanceOf(AttachmentUploadCancelledError);
    expect(fake.calls).toHaveLength(0);
  });

  test("the computer's refusal is the error the person reads", async () => {
    const fake = fakeHost();
    const result = uploadFile(fake.host, server, file);
    await started(fake);
    fake.answer(413, "File is larger than this computer accepts.\n");
    await expect(result).rejects.toThrow("File is larger than this computer accepts.");
  });

  test("a file over the 2 GiB limit is refused before anything is sent", async () => {
    const fake = fakeHost();
    await expect(uploadFile(fake.host, server, { ...file, size: 3 * 1024 ** 3 })).rejects.toThrow("File exceeds the 2 GiB upload limit.");
    expect(fake.calls).toHaveLength(0);
  });
});
