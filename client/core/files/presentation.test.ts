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
import { downloadSessionFile, SESSION_FILE_BINARY_LIMIT_BYTES, type SessionFileDownloadHost } from "./sessionFilePreviewDownload";

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
  previewLimitBytes: SESSION_FILE_BINARY_LIMIT_BYTES,
  ...overrides,
});

describe("file preview words", () => {
  test("the header names the file, where it is, what it is and how big", () => {
    expect(sessionFileMetaLine(metadata(), "out/report.pdf")).toBe("out/report.pdf · PDF · 2.0 KB");
    expect(sessionFileMetaLine(null, "")).toBe("Current Session");
    expect(sessionFileTitle(null, "src/app/main.go")).toBe("main.go");
    expect(sessionFileTitle(null, "")).toBe("File");
    expect(sessionFileKindLabel(metadata({ kind: "text", contentType: "application/json" }))).toBe("JSON");
    expect(sessionFileKindLabel(metadata({ kind: "text", contentType: "application/yaml" }))).toBe("YAML");
    expect(sessionFileKindLabel(metadata({ kind: "text", contentType: "text/plain", name: "run.LOG" }))).toBe("Log");
    expect(sessionFileKindLabel(metadata({ kind: "unsupported" }))).toBe("Binary");
  });

  test("states, truncation and download feedback", () => {
    expect(sessionFileProblemTitle({ stale: true, tooLarge: true })).toBe("File changed");
    expect(sessionFileProblemTitle({ stale: false, tooLarge: true })).toBe("File too large");
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
      maxBytes: SESSION_FILE_BINARY_LIMIT_BYTES,
      headers: { "Cache-Control": "no-store" },
    });
    expect(h.requests[0]!.names.slice(0, 3)).toEqual(["report.pdf", "report (1).pdf", "report (2).pdf"]);
  });

  test("a closed picker signs nothing; a file over the limit asks nothing", async () => {
    const h = host(null);
    let signed = false;
    expect(await downloadSessionFile({ host: h.value, metadata: metadata(), sign: async () => ((signed = true), { uri: "", headers: {} }) })).toBe("cancelled");
    expect(signed).toBe(false);
    await expect(downloadSessionFile({ host: h.value, metadata: metadata({ size: SESSION_FILE_BINARY_LIMIT_BYTES + 1 }), sign: async () => ({ uri: "", headers: {} }) })).rejects.toThrow("This file can't be downloaded.");
    expect(h.order).toEqual(["pick"]);
  });
});
