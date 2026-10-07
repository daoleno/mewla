import { describe, expect, test } from "bun:test";
import { createWebUploadTask } from "./webUploadTask.web";

class FakeXHR {
  static last: FakeXHR;
  method = "";
  url = "";
  headers: Record<string, string> = {};
  body: unknown;
  status = 0;
  responseText = "";
  aborted = false;
  upload: { onprogress?: (event: ProgressEvent) => void } = {};
  onload?: () => void;
  onerror?: () => void;
  onabort?: () => void;
  constructor() {
    FakeXHR.last = this;
  }
  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(name: string, value: string) {
    this.headers[name] = value;
  }
  send(body: unknown) {
    this.body = body;
  }
  abort() {
    this.aborted = true;
    this.onabort?.();
  }
}
Object.assign(globalThis, { XMLHttpRequest: FakeXHR });

describe("browser attachment upload", () => {
  test("posts the picked File itself with auth headers and reports byte progress", async () => {
    const file = new Blob(["%PDF-1.4"], { type: "application/pdf" });
    const progress: unknown[] = [];
    const task = createWebUploadTask({
      file,
      uploadUrl: "https://manjaro.example/upload",
      headers: { Authorization: "Zen a", "X-Zen-Upload-Name": "a.pdf" },
      onProgress: (value) => progress.push(value),
    });
    const result = task.uploadAsync();
    const xhr = FakeXHR.last;
    expect([xhr.method, xhr.url, xhr.body]).toEqual([
      "POST",
      "https://manjaro.example/upload",
      file,
    ]);
    expect(xhr.headers).toEqual({ Authorization: "Zen a", "X-Zen-Upload-Name": "a.pdf" });
    xhr.upload.onprogress?.({ loaded: 4, total: 8, lengthComputable: true } as ProgressEvent);
    xhr.status = 200;
    xhr.responseText = '{"path":"/state/uploads/a.pdf"}';
    xhr.onload?.();
    expect(await result).toEqual({
      status: 200,
      body: '{"path":"/state/uploads/a.pdf"}',
      headers: {},
    });
    expect(progress).toEqual([{ bytesSent: 4, totalBytes: 8 }]);
  });

  test("a network failure rejects instead of reporting a status-0 success", async () => {
    const task = createWebUploadTask({
      file: new Blob(["x"]),
      uploadUrl: "https://manjaro.example/upload",
      headers: {},
      onProgress() {},
    });
    const result = task.uploadAsync();
    FakeXHR.last.onerror?.();
    await expect(result).rejects.toThrow("Upload could not reach Mewla");
  });

  test("cancel aborts the in-flight request", async () => {
    const task = createWebUploadTask({
      file: new Blob(["x"]),
      uploadUrl: "https://manjaro.example/upload",
      headers: {},
      onProgress() {},
    });
    const result = task.uploadAsync();
    task.cancel();
    expect(FakeXHR.last.aborted).toBe(true);
    await expect(result).rejects.toThrow("cancelled");
  });
});
