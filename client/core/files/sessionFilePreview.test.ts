import { describe, expect, test } from "bun:test";
import {
  appendSessionFileCapabilityQuery,
  bindSessionFileRequestToGeneration,
  buildSessionFileBinaryUrl,
  classifySessionFileRenderer,
  normalizeSessionFileMetadata,
  normalizeSessionFileText,
  recognizeSessionFileReference,
  sessionFileStartedAt,
  sessionFileTooLargeMessage,
} from "./sessionFilePreview";

describe("current-Session file reference recognition", () => {
  test("recognizes explicit local Markdown destinations and common inline-code paths", () => {
    expect(recognizeSessionFileReference("/repo/mewla/docs/notes.md#L12")).toBe(
      "/repo/mewla/docs/notes.md",
    );
    expect(recognizeSessionFileReference("./app/index.tsx:41:7")).toBe(
      "./app/index.tsx",
    );
    expect(recognizeSessionFileReference("README.md")).toBe("README.md");
    expect(recognizeSessionFileReference("file:///repo/My%20File.pdf")).toBe(
      "/repo/My File.pdf",
    );
    expect(recognizeSessionFileReference("C:\\repo\\mewla\\main.go:18")).toBe(
      "C:\\repo\\mewla\\main.go",
    );
  });

  test("does not steal external, executable, anchor, or prose links", () => {
    for (const value of [
      "https://example.com/file.md",
      "mailto:user@example.com",
      "javascript:alert(1)",
      "#section",
      "release-notes",
      "",
    ]) {
      expect(recognizeSessionFileReference(value)).toBeNull();
    }
  });
});

describe("current-Session file renderer contract", () => {
  test("projects every supported wire kind to one shared mobile renderer", () => {
    expect(classifySessionFileRenderer("markdown")).toBe("markdown");
    expect(classifySessionFileRenderer("text")).toBe("text");
    expect(classifySessionFileRenderer("image")).toBe("image");
    expect(classifySessionFileRenderer("pdf")).toBe("pdf");
    expect(classifySessionFileRenderer("unsupported")).toBe("unsupported");
  });

  test("builds the authenticated streaming endpoint without route ownership", () => {
    const url = buildSessionFileBinaryUrl("wss://host.example/ws", {
      workerId: "%152",
      processId: 412,
      startedAt: 1_784_518_400_123,
      path: "docs/guide.pdf",
      generation: "generation-token",
    });
    expect(url).toBe(
      "https://host.example/session-file?worker_id=%25152&process_id=412&started_at=1784518400123&path=docs%2Fguide.pdf&generation=generation-token",
    );
    expect(url).not.toContain("/terminal/");
  });

  test("binds follow-up reads to the daemon's exact canonical host path", () => {
    const metadata = normalizeSessionFileMetadata({
      name: "shared.md",
      path: "/host/other-repo/shared.md",
      relative_path: "../other-repo/shared.md",
      kind: "markdown",
      content_type: "text/markdown; charset=utf-8",
      size: 9,
      modified_at: "2026-07-20T04:00:00Z",
      generation: "generation-token",
      too_large: false,
      preview_limit_bytes: 0,
    });

    expect(
      bindSessionFileRequestToGeneration(
        {
          workerId: "main:@7",
          processId: 412,
          startedAt: 1_784_518_400_123,
          path: "shared-alias.md",
        },
        metadata,
      ),
    ).toEqual({
      workerId: "main:@7",
      processId: 412,
      startedAt: 1_784_518_400_123,
      path: "/host/other-repo/shared.md",
      generation: "generation-token",
    });
  });

  test("carries repeatable method-scoped capabilities without a nonce authorization", () => {
    const sourceUrl = appendSessionFileCapabilityQuery(
      "https://host.example/session-file?path=docs%2Foverview.webp&generation=token",
      {
        deviceId: "device-a",
        expiresAtMS: 1_900_000_000_000,
        getSignature: "a".repeat(128),
        headSignature: "b".repeat(128),
      },
    );
    const query = new URL(sourceUrl).searchParams;

    expect(query.has("auth")).toBe(false);
    expect(query.get("file_cap_device")).toBe("device-a");
    expect(query.get("file_cap_expires")).toBe("1900000000000");
    expect(query.get("file_cap_get")).toBe("a".repeat(128));
    expect(query.get("file_cap_head")).toBe("b".repeat(128));
    expect(sourceUrl).toContain("path=docs%2Foverview.webp");
    expect(sourceUrl).toContain("generation=token");
  });

  test("rejects an oversized text payload even if a server violates its bound", () => {
    expect(() =>
      normalizeSessionFileText({
        content: "x".repeat(512 * 1024 + 1),
        bytes_read: 512 * 1024 + 1,
        truncated: false,
        generation: "generation-token",
      }),
    ).toThrow("oversized text preview");
  });

  test("keeps the daemon-owned binary size limit as an explicit no-download state", () => {
    const metadata = normalizeSessionFileMetadata({
      name: "manual.pdf",
      path: "/repo/manual.pdf",
      relative_path: "manual.pdf",
      kind: "pdf",
      content_type: "application/pdf",
      size: 50 * 1024 * 1024 + 1,
      modified_at: "2026-07-20T04:00:00Z",
      generation: "generation-token",
      too_large: true,
      preview_limit_bytes: 50 * 1024 * 1024,
    });

    expect(metadata.tooLarge).toBe(true);
    expect(metadata.previewLimitBytes).toBe(50 * 1024 * 1024);
    expect(sessionFileTooLargeMessage(metadata)).toBe(
      "This 50 MB file is over Mewla's 50 MB preview limit.",
    );
  });
});

describe("a run's start for the file routes", () => {
  test("Brain's host reports an RFC 3339 time with nanoseconds: milliseconds, cut not rounded", () => {
    expect(sessionFileStartedAt("2026-11-15T10:46:19.999999999Z")).toBe(Date.UTC(2026, 10, 15, 10, 46, 19, 999));
    expect(sessionFileStartedAt("2026-11-15T18:46:19.5+08:00")).toBe(Date.UTC(2026, 10, 15, 10, 46, 19, 500));
  });

  test("numbers in seconds or milliseconds, and nothing usable as zero", () => {
    expect(sessionFileStartedAt(1_762_840_800_000)).toBe(1_762_840_800_000);
    expect(sessionFileStartedAt(1_762_840_800)).toBe(1_762_840_800_000);
    expect(sessionFileStartedAt("soon")).toBe(0);
    expect(sessionFileStartedAt(undefined)).toBe(0);
  });
});
