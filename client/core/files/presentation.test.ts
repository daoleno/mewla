import { describe, expect, test } from "bun:test";
import {
  sessionFileDownloadLabel,
  sessionFileDownloadStatus,
  sessionFileKindLabel,
  sessionFileMetaLine,
  sessionFileProblemTitle,
  sessionFileTitle,
  sessionFileTruncationNote,
} from "./presentation";
import type { SessionFileMetadata } from "./sessionFilePreview";
import { downloadSessionFile, type SessionFileDownloadHost } from "./sessionFilePreviewDownload";

const metadata = (overrides: Partial<SessionFileMetadata> = {}): SessionFileMetadata => ({
  name: "report.pdf",
  path: "/work/out/report.pdf",
  relativePath: "out/report.pdf",
  kind: "pdf",
  contentType: "application/pdf",
  size: 2048,
  modifiedAt: "2026-11-11T08:00:00Z",
  generation: "g1",
  tooLarge: false,
  previewLimitBytes: 50 * 1024 * 1024,
  ...overrides,
});

describe("file preview words", () => {
  test("the header names the file, where it is, what it is and how big", () => {
    expect(sessionFileMetaLine(metadata(), "out/report.pdf")).toBe("out/report.pdf · PDF · 2.0 KB");
    expect(sessionFileMetaLine(null, "")).toBe("Current Session");
    expect(sessionFileMetaLine(metadata({ kind: "text", name: "report.html", contentType: "text/html; charset=utf-8", relativePath: "report.html" }), "")).toBe("report.html · HTML · 2.0 KB");
    // A file Brain named outside its workspace reads as its path, not a climb of "../".
    expect(sessionFileMetaLine(metadata({ path: "/home/me/out/report.pdf", relativePath: "../../out/report.pdf" }), "")).toBe("/home/me/out/report.pdf · PDF · 2.0 KB");
    expect(sessionFileTitle(null, "src/app/main.go")).toBe("main.go");
    expect(sessionFileTitle(null, "")).toBe("File");
    expect(sessionFileKindLabel(metadata({ kind: "text", contentType: "application/json" }))).toBe("JSON");
    expect(sessionFileKindLabel(metadata({ kind: "text", contentType: "application/yaml" }))).toBe("YAML");
    expect(sessionFileKindLabel(metadata({ kind: "text", contentType: "text/plain", name: "run.LOG" }))).toBe("Log");
    expect(sessionFileKindLabel(metadata({ kind: "unsupported" }))).toBe("Binary");
  });

  test("states, truncation and download feedback", () => {
    expect(sessionFileProblemTitle({ stale: true, tooLarge: true })).toBe("File changed");
    expect(sessionFileProblemTitle({ stale: false, tooLarge: true })).toBe("Too large to preview");
    expect(sessionFileProblemTitle({ stale: false, tooLarge: false })).toBe("Could not open file");
    expect(sessionFileTruncationNote({ content: "x", bytesRead: 512 * 1024, truncated: true, generation: "g" })).toBe("Preview limited to the first 512 KB.");
    expect(sessionFileTruncationNote({ content: "x", bytesRead: 1, truncated: false, generation: "g" })).toBeNull();
    expect(sessionFileDownloadStatus("saved", "")).toBe("Saved");
    expect(sessionFileDownloadStatus("failed", "")).toBe("Download failed: Unknown error");
    expect(sessionFileDownloadStatus("busy", "")).toBeNull();
    expect(sessionFileDownloadLabel({ busy: false, can: false })).toBe("Download unavailable");
  });
});

describe("download through the host", () => {
  function host(folder: { token: string } | null) {
    const order: string[] = [];
    const requests: Parameters<SessionFileDownloadHost["download"]>[0][] = [];
    const value: SessionFileDownloadHost = {
      pickDirectory: async () => (order.push("pick"), folder),
      download: async (request) => (order.push("download"), requests.push(request), { bytesWritten: request.expectedSize ?? 0, name: request.names[0]! }),
    };
    return { value, order, requests };
  }

  test("picks the folder, then signs, then streams with the limits and every free-name candidate", async () => {
    const h = host({ token: "folder-1" });
    const sign = async () => (h.order.push("sign"), { uri: "http://desk.test:9877/session-file?x=1", headers: { "Cache-Control": "no-store" } });
    expect(await downloadSessionFile({ host: h.value, metadata: metadata(), sign })).toBe("saved");
    expect(h.order).toEqual(["pick", "sign", "download"]);
    expect(h.requests[0]).toMatchObject({
      url: "http://desk.test:9877/session-file?x=1",
      directory: "folder-1",
      mimeType: "application/pdf",
      expectedSize: 2048,
      maxBytes: 2048,
      headers: { "Cache-Control": "no-store" },
    });
    expect(h.requests[0]!.names.slice(0, 3)).toEqual(["report.pdf", "report (1).pdf", "report (2).pdf"]);
  });

  test("a closed picker signs nothing", async () => {
    const h = host(null);
    let signed = false;
    expect(await downloadSessionFile({ host: h.value, metadata: metadata(), sign: async () => ((signed = true), { uri: "", headers: {} }) })).toBe("cancelled");
    expect(signed).toBe(false);
    expect(h.order).toEqual(["pick"]);
  });

  test("a file past the preview limit downloads all the same, held to the size it was inspected at", async () => {
    const h = host({ token: "folder-1" });
    const size = 3 * 1024 * 1024 * 1024;
    const big = metadata({ name: "model.gguf", path: "/work/model.gguf", kind: "unsupported", contentType: "application/octet-stream", size, tooLarge: true });
    expect(await downloadSessionFile({ host: h.value, metadata: big, sign: async () => ({ uri: "http://desk.test/f", headers: {} }) })).toBe("saved");
    expect(h.requests[0]).toMatchObject({ expectedSize: size, maxBytes: size });
  });

  test("a host that hands the download to the browser reports it as started, not saved", async () => {
    const h = host({ token: "browser-downloads" });
    const handoff: SessionFileDownloadHost = { pickDirectory: h.value.pickDirectory, download: async (request) => ({ bytesWritten: request.expectedSize ?? 0, name: request.names[0]!, handedOff: true }) };
    expect(await downloadSessionFile({ host: handoff, metadata: metadata(), sign: async () => ({ uri: "http://desk.test/f", headers: {} }) })).toBe("started");
  });

  test("an empty file still gets a positive byte bound", async () => {
    const h = host({ token: "folder-1" });
    await downloadSessionFile({ host: h.value, metadata: metadata({ size: 0 }), sign: async () => ({ uri: "http://desk.test/f", headers: {} }) });
    expect(h.requests[0]).toMatchObject({ expectedSize: 0, maxBytes: 1 });
  });
});

test("a file deleted or moved since the Session wrote it is gone", () => {
  expect(sessionFileProblemTitle({ stale: false, tooLarge: false, gone: true })).toBe("File is gone");
  expect(sessionFileProblemTitle({ stale: true, tooLarge: false })).toBe("File changed");
});
