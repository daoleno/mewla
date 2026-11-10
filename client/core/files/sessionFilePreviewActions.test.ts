import { describe, expect, test } from "bun:test";
import {
  createSessionFileCopyFeedback,
  createSessionFileCopyLifecycleOwner,
  resolveSessionFileCopyContent,
  SESSION_FILE_COPIED_RESET_MS,
} from "./sessionFilePreviewCopy";
import {
  createSessionFileDownloadLifecycleOwner,
  isSessionFileDownloadCancelError,
  SESSION_FILE_BINARY_LIMIT_BYTES,
  sessionFileCanDownload,
  sessionFileDownloadFileName,
  sessionFileDownloadMimeType,
  sessionFileDownloadNameCandidates,
  sessionFileDownloadRequest,
} from "./sessionFilePreviewDownload";
import { normalizeSessionFileMetadata } from "./sessionFilePreview";

describe("Session file preview copy feedback", () => {
  test("shows success only after the clipboard write resolves", async () => {
    const states: boolean[] = [];
    let finishWrite: (() => void) | undefined;
    let reset: (() => void) | undefined;
    let resetDelay: number | undefined;
    const feedback = createSessionFileCopyFeedback({
      copyText: () =>
        new Promise<void>((resolve) => {
          finishWrite = resolve;
        }),
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback, delayMs) => {
        reset = callback;
        resetDelay = delayMs;
        return Symbol("timer");
      },
      cancelReset: () => {},
    });

    const request = feedback.copy("/repo/notes.md");
    expect(states).toEqual([]);

    finishWrite?.();
    await request;
    expect(states).toEqual([true]);
    expect(resetDelay).toBe(SESSION_FILE_COPIED_RESET_MS);

    reset?.();
    expect(states).toEqual([true, false]);
  });

  test("failed clipboard writes stay quiet and later success recovers", async () => {
    const states: boolean[] = [];
    let attempts = 0;
    const feedback = createSessionFileCopyFeedback({
      copyText: async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("clipboard unavailable");
        }
      },
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await expect(feedback.copy("/repo/a.md")).resolves.toBeUndefined();
    expect(states).toEqual([]);

    await feedback.copy("/repo/b.md");
    expect(attempts).toBe(2);
    expect(states).toEqual([true]);
  });

  test("repeated successes replace the reset without stale timer races", async () => {
    const states: boolean[] = [];
    const resets: Array<() => void> = [];
    const canceled: number[] = [];
    const feedback = createSessionFileCopyFeedback({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: (callback) => {
        resets.push(callback);
        return resets.length - 1;
      },
      cancelReset: (timer) => {
        canceled.push(timer);
      },
    });

    await feedback.copy("first");
    await feedback.copy("second");

    expect(states).toEqual([true, true]);
    expect(canceled).toEqual([0]);

    resets[0]?.();
    expect(states).toEqual([true, true]);
    feedback.dispose();
    expect(canceled).toEqual([0, 1]);
    resets[1]?.();
    expect(states).toEqual([true, true]);
  });

  test("dispose cancels reset and blocks late async feedback", async () => {
    const states: boolean[] = [];
    const canceled: symbol[] = [];
    let finishLateWrite: (() => void) | undefined;
    const timer = Symbol("timer");
    const feedback = createSessionFileCopyFeedback({
      copyText: (text) =>
        text === "late"
          ? new Promise<void>((resolve) => {
              finishLateWrite = resolve;
            })
          : Promise.resolve(),
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => timer,
      cancelReset: (value) => {
        canceled.push(value);
      },
    });

    await feedback.copy("first");
    const lateRequest = feedback.copy("late");
    feedback.dispose();
    finishLateWrite?.();
    await lateRequest;

    expect(canceled).toEqual([timer]);
    expect(states).toEqual([true]);
  });
});

describe("Session file preview copy content", () => {
  test("an untruncated preview copies without loading the file again", async () => {
    let loaded = 0;
    const content = await resolveSessionFileCopyContent(
      {
        content: "full text",
        bytesRead: 9,
        truncated: false,
        generation: "generation-1",
      },
      async () => {
        loaded += 1;
        return "whole file";
      },
    );
    expect(content).toBe("full text");
    expect(loaded).toBe(0);
  });

  test("a truncated preview copies the full text fetched the download way", async () => {
    const content = await resolveSessionFileCopyContent(
      {
        content: "partial",
        bytesRead: 7,
        truncated: true,
        generation: "generation-1",
      },
      async () => "whole file",
    );
    expect(content).toBe("whole file");
  });

  test("no text preview has nothing to copy", async () => {
    let loaded = 0;
    const content = await resolveSessionFileCopyContent(null, async () => {
      loaded += 1;
      return "whole file";
    });
    expect(content).toBeNull();
    expect(loaded).toBe(0);
  });
});

describe("Session file preview copy lifecycle owner", () => {
  test("file/request replace installs a fresh controller so later copies still Check", async () => {
    const states: boolean[] = [];
    const owner = createSessionFileCopyLifecycleOwner({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await owner.copy("/repo/first.md");
    expect(states).toEqual([true]);

    owner.replaceController();
    expect(states).toEqual([true, false]);

    await owner.copy("/repo/second.md");
    expect(states).toEqual([true, false, true]);

    owner.replaceController();
    await owner.copy("/repo/retry.md");
    expect(states).toEqual([true, false, true, false, true]);
  });

  test("dispose-then-reuse of a single controller stays dead; owner replace recovers", async () => {
    const states: boolean[] = [];
    let copyTextCalls = 0;
    const options = {
      copyText: async () => {
        copyTextCalls += 1;
      },
      onCopiedChange: (copied: boolean) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    };

    const dead = createSessionFileCopyFeedback(options);
    await dead.copy("before-dispose");
    expect(states).toEqual([true]);
    dead.dispose();
    await dead.copy("after-dispose");
    expect(copyTextCalls).toBe(2);
    expect(states).toEqual([true]);

    const owner = createSessionFileCopyLifecycleOwner(options);
    owner.replaceController();
    await owner.copy("after-replace");
    expect(states.at(-1)).toBe(true);
    expect(copyTextCalls).toBe(3);
  });

  test("owner dispose blocks further Check feedback", async () => {
    const states: boolean[] = [];
    const owner = createSessionFileCopyLifecycleOwner({
      copyText: async () => {},
      onCopiedChange: (copied) => {
        states.push(copied);
      },
      scheduleReset: () => Symbol("timer"),
      cancelReset: () => {},
    });

    await owner.copy("live");
    expect(states).toEqual([true]);
    owner.dispose();
    expect(states).toEqual([true, false]);
    await owner.copy("after-owner-dispose");
    expect(states).toEqual([true, false]);
  });
});

function bytesStream(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk);
      controller.close();
    },
  });
}

function collectingSink(store: {
  chunks: Uint8Array[];
  aborted?: unknown;
}): {
  writableStream(): WritableStream<Uint8Array>;
} {
  return {
    writableStream() {
      return new WritableStream<Uint8Array>({
        write(chunk) {
          store.chunks.push(chunk);
        },
        abort(reason) {
          store.aborted = reason;
        },
      });
    },
  };
}

describe("Session file preview download", () => {
  const metadata = normalizeSessionFileMetadata({
    name: "guide.md",
    path: "/host/repo/docs/guide.md",
    relative_path: "docs/guide.md",
    kind: "markdown",
    content_type: "text/markdown; charset=utf-8",
    size: 420,
    modified_at: "2026-08-06T04:00:00Z",
    generation: "generation-token",
    too_large: false,
    preview_limit_bytes: 0,
  });

  test("sanitizes download filenames, mime types, and binds the exact metadata path", () => {
    expect(
      sessionFileDownloadFileName({
        name: "../evil/name.md",
        path: "/host/repo/docs/guide.md",
      }),
    ).toBe(".._evil_name.md");
    expect(
      sessionFileDownloadFileName({
        name: "Quarterly report - Databricks.pdf",
        path: "/host/repo/reports/Quarterly report - Databricks.pdf",
      }),
    ).toBe("Quarterly report.pdf");
    expect(
      sessionFileDownloadFileName({
        name: "Architecture notes | Notion",
        path: "/host/repo/docs/architecture-notes.md",
      }),
    ).toBe("Architecture notes.md");
    expect(
      sessionFileDownloadFileName({
        name: "My report - custom.pdf",
        path: "/host/repo/My report - custom.pdf",
      }),
    ).toBe("My report - custom.pdf");
    expect(
      sessionFileDownloadFileName({
        name: "draft.txt",
        path: "/host/repo/draft.txt\u0000",
      }),
    ).toBe("draft.txt");
    expect(sessionFileDownloadMimeType(metadata.contentType)).toBe(
      "text/markdown",
    );
    expect(sessionFileDownloadMimeType("")).toBe("application/octet-stream");
    expect(
      sessionFileDownloadRequest(
        {
          workerId: "main:@7",
          processId: 412,
          startedAt: 1_784_518_400_123,
        },
        metadata,
      ),
    ).toEqual({
      workerId: "main:@7",
      processId: 412,
      startedAt: 1_784_518_400_123,
      path: "/host/repo/docs/guide.md",
      generation: "generation-token",
    });
  });

  test("download eligibility stays inside the existing binary size bound", () => {
    expect(sessionFileCanDownload(metadata)).toBe(true);
    expect(
      sessionFileCanDownload({
        ...metadata,
        size: SESSION_FILE_BINARY_LIMIT_BYTES + 1,
        tooLarge: false,
      }),
    ).toBe(false);
    expect(sessionFileCanDownload(null)).toBe(false);
  });

  test("a taken name tries name (1), name (2) … in that order, extension kept", () => {
    expect(sessionFileDownloadNameCandidates("guide.md", 3)).toEqual(["guide.md", "guide (1).md", "guide (2).md", "guide (3).md"]);
    expect(sessionFileDownloadNameCandidates("Makefile", 1)).toEqual(["Makefile", "Makefile (1)"]);
    expect(sessionFileDownloadNameCandidates("archive.tar.gz", 1)).toEqual(["archive.tar.gz", "archive.tar (1).gz"]);
  });

  test("picker and download cancellations read as cancelled, other failures do not", () => {
    expect(isSessionFileDownloadCancelError(new Error("Directory picking was cancelled"))).toBe(true);
    expect(isSessionFileDownloadCancelError(new Error("Session file download was cancelled."))).toBe(true);
    expect(isSessionFileDownloadCancelError(new Error("Session file download failed (HTTP 500)."))).toBe(false);
  });
});

describe("Session file preview download lifecycle", () => {
  test("ignores duplicate taps while active and settles success", async () => {
    const states: string[] = [];
    let resolveTask: ((result: "saved") => void) | undefined;
    const owner = createSessionFileDownloadLifecycleOwner({
      onFeedbackChange: (state) => states.push(state),
    });
    const first = owner.start(
      () => new Promise<"saved">((resolve) => (resolveTask = resolve)),
    );
    const duplicate = owner.start(async () => "saved");

    expect(await duplicate).toBeUndefined();
    expect(states).toEqual(["busy"]);
    resolveTask?.("saved");
    await expect(first).resolves.toBe("saved");
    expect(states).toEqual(["busy", "saved"]);
  });

  test("reports failure, resets after cancellation, and can retry", async () => {
    const states: string[] = [];
    const errors: unknown[] = [];
    let attempts = 0;
    const owner = createSessionFileDownloadLifecycleOwner({
      onFeedbackChange: (state, error) => {
        states.push(state);
        if (error !== undefined) errors.push(error);
      },
    });

    await expect(
      owner.start(async () => {
        attempts += 1;
        throw new Error("write failed");
      }),
    ).rejects.toThrow("write failed");
    expect(states).toEqual(["busy", "failed"]);
    expect(errors).toEqual([expect.any(Error)]);
    expect((errors[0] as Error).message).toBe("write failed");

    await expect(owner.start(async () => "cancelled")).resolves.toBe(
      "cancelled",
    );
    expect(states).toEqual(["busy", "failed", "busy", "idle"]);

    await expect(owner.start(async () => "saved")).resolves.toBe("saved");
    expect(attempts).toBe(1);
    expect(states.at(-1)).toBe("saved");
  });
});
