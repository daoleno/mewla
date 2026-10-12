import { afterEach, describe, expect, test } from "bun:test";
import { createSessionFileDownloadBackend } from "./sessionFilePreviewDownloadBackend.web";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

interface DeliveredDownload {
  readonly fileName: string;
  readonly blob: Blob;
}

function createDeliveredRecorder() {
  const delivered: DeliveredDownload[] = [];
  return {
    delivered,
    sink: {
      deliver(blob: Blob, fileName: string) {
        delivered.push({ fileName, blob });
      },
    },
  };
}

function stubFetch(input: {
  body?: string;
  status?: number;
  requests?: Array<{ url: string; headers: Record<string, string> }>;
}) {
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    input.requests?.push({
      url: String(url),
      headers: (init.headers ?? {}) as Record<string, string>,
    });
    return new Response(input.body ?? "", { status: input.status ?? 200 });
  }) as typeof globalThis.fetch;
}

describe("Web Session file download backend", () => {
  test("fetches with the source headers and delivers the reserved name and bytes", async () => {
    const requests: Array<{ url: string; headers: Record<string, string> }> = [];
    stubFetch({ body: "hello", requests });
    const { delivered, sink } = createDeliveredRecorder();
    const backend = createSessionFileDownloadBackend(sink);

    const directory = await backend.pickDirectory();
    const destination = directory.reserve("system-prompt.md", "text/markdown");
    await backend.download(
      "https://host.example/session-file?cap=1",
      destination,
      {
        headers: { "Cache-Control": "no-store" },
        expectedBytes: 5,
      },
    );

    expect(requests).toEqual([
      {
        url: "https://host.example/session-file?cap=1",
        headers: { "Cache-Control": "no-store" },
      },
    ]);
    expect(delivered).toHaveLength(1);
    expect(delivered[0].fileName).toBe("system-prompt.md");
    expect(delivered[0].blob.type).toBe("text/markdown");
    expect(await delivered[0].blob.text()).toBe("hello");
  });

  test("a non-2xx response throws and delivers nothing", async () => {
    stubFetch({ status: 500, body: "nope" });
    const { delivered, sink } = createDeliveredRecorder();
    const backend = createSessionFileDownloadBackend(sink);

    const directory = await backend.pickDirectory();
    const destination = directory.reserve("notes.md", "text/plain");
    await expect(
      backend.download("https://host.example/session-file", destination, {
        headers: {},
        expectedBytes: 4,
      }),
    ).rejects.toThrow("HTTP 500");
    expect(delivered).toHaveLength(0);
  });

  test("a truncation mismatch throws before any browser download starts", async () => {
    stubFetch({ body: "hello" });
    const { delivered, sink } = createDeliveredRecorder();
    const backend = createSessionFileDownloadBackend(sink);

    const directory = await backend.pickDirectory();
    const destination = directory.reserve("notes.md", "text/plain");
    await expect(
      backend.download("https://host.example/session-file", destination, {
        headers: {},
        expectedBytes: 7,
      }),
    ).rejects.toThrow("truncated");
    expect(delivered).toHaveLength(0);
  });
});
