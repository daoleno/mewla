import { describe, expect, open, test, type StubResponse } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";
const UPLOADS = "./testing/Uploads.tsx";
const BASE = "http://desk.test:9877";
const identity = { workerId: "w1", processId: 42, startedAt: 1_700_000_000 };

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const meta = (overrides: Record<string, unknown> = {}) => ({
  name: "notes.txt",
  path: "/work/notes.txt",
  relative_path: "notes.txt",
  kind: "text",
  content_type: "text/plain",
  size: 600 * 1024,
  modified_at: "2026-11-11T08:00:00Z",
  generation: "g1",
  too_large: false,
  preview_limit_bytes: 50 * 1024 * 1024,
  ...overrides,
});

const capability = () => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify({ version: 1, device_id: "phone-1", expires_at_ms: Date.now() + 120_000, get_signature: "a".repeat(128), head_signature: "b".repeat(128) }) });

type Asked = { type: string; body: Record<string, unknown> };

/** A Session file preview against a computer whose answers come from `files`. */
function preview(metadata: Record<string, unknown>, extra: { handlers?: Record<string, unknown>; network?: Record<string, StubResponse | (() => StubResponse)> } = {}) {
  const asked: Asked[] = [];
  return {
    asked,
    options: {
      host: {
        params: { page: { name: "files-preview", reference: String(metadata.relative_path), identity } },
        handlers: {
          ...secureStore(),
          "app.daemon": ({ type, body }: Asked) => {
            if (type.startsWith("session_file")) asked.push({ type, body });
            if (type === "session_file_metadata") return { reply: "session_file_metadata", metadata };
            if (type === "session_file_text") return { reply: "session_file_text", text: { content: "first line\nsecond line", bytes_read: 512 * 1024, truncated: true, generation: "g1" } };
            return { error: `unexpected ${type}` };
          },
          ...extra.handlers,
        },
      },
      network: extra.network ?? {},
    },
  };
}

describe("File preview", () => {
  test("a text file shows as text, says its preview was cut, and asked the live Session for this generation", async () => {
    const c = preview(meta());
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:file-text");
    expect(app.texts()).toContain("first line\nsecond line");
    expect(app.texts()).toContain("Preview limited to the first 512 KB.");
    expect(app.texts()).toContain("notes.txt · Text · 600 KB");
    expect(c.asked[0]).toEqual({ type: "session_file_metadata", body: { worker_id: "w1", process_id: 42, started_at: 1_700_000_000, path: "notes.txt" } });
    expect(c.asked[1]).toEqual({ type: "session_file_text", body: { worker_id: "w1", process_id: 42, started_at: 1_700_000_000, path: "/work/notes.txt", file_generation: "g1" } });
    expect(app.shows("id:file-copy")).toBe(true);
  });

  test("a PDF signs a read capability for its generation, then shows its pages", async () => {
    const c = preview(meta({ name: "report.pdf", path: "/work/report.pdf", relative_path: "out/report.pdf", kind: "pdf", content_type: "application/pdf", size: 4096 }), {
      network: { [`POST ${BASE}/session-file-capability`]: capability() },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:file-pdf");
    const signed = app.requests().filter((r) => r.url === `${BASE}/session-file-capability`);
    expect(signed).toHaveLength(1);
    expect(JSON.parse(String(signed[0]!.body))).toEqual({ worker_id: "w1", process_id: 42, started_at: 1_700_000_000, path: "/work/report.pdf", generation: "g1" });
    expect(signed[0]!.headers.authorization ?? signed[0]!.headers.Authorization).toMatch(/v1:/);
    expect(c.asked.map((a) => a.type)).toEqual(["session_file_metadata"]);
    expect(app.texts()).toContain("out/report.pdf · PDF · 4.0 KB");
  });

  test("a file over the limit says so, reads nothing, and offers no download", async () => {
    const c = preview(meta({ name: "dump.bin", relative_path: "dump.bin", kind: "unsupported", size: 3 * 1024 * 1024 * 1024, too_large: true }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:file-problem");
    expect(app.texts()).toContain("File too large");
    expect(app.texts()).toContain("This 3.0 GB file exceeds Mewla's 50 MB preview limit. It was not downloaded.");
    expect(c.asked.map((a) => a.type)).toEqual(["session_file_metadata"]);
    expect(app.enabled("id:file-download")).toBe(false);
  });

  test("a file that changed under the preview says so and can be read again", async () => {
    let changed = true;
    const c = preview(meta());
    c.options.host.handlers["app.daemon"] = ({ type }: Asked) => {
      if (type === "session_file_metadata" && changed) return { error: "The file changed while it was being read.", code: "session_file_changed" };
      if (type === "session_file_metadata") return { reply: "session_file_metadata", metadata: meta() };
      return { reply: "session_file_text", text: { content: "fresh", bytes_read: 5, truncated: false, generation: "g1" } };
    };
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:File changed");
    expect(app.shows("id:file-refresh")).toBe(true);
    changed = false;
    await app.press("id:file-problem-action");
    await app.waitFor("text:fresh");
    expect(app.shows("id:file-truncated")).toBe(false);
  });

  test("Download asks for a folder first, then signs and hands the host the stream, limits and free names", async () => {
    let signedAtPick = -1;
    let app: Awaited<ReturnType<typeof open>> | null = null;
    const c = preview(meta(), {
      handlers: {
        "files.pickDirectory": () => {
          signedAtPick = app!.requests().filter((r) => r.url === `${BASE}/session-file-capability`).length;
          return { token: "folder-1", name: "Downloads" };
        },
        "files.download": (request: { names: string[]; expectedSize: number }) => ({ bytesWritten: request.expectedSize, name: request.names[0] }),
      },
      network: { [`POST ${BASE}/session-file-capability`]: capability() },
    });
    app = await open(HARNESS, c.options);
    await app.waitFor("id:file-text");
    await app.press("id:file-download");
    await app.waitFor("text:Saved");
    expect(signedAtPick).toBe(0);
    const call = app.host.calls().find((m) => m.name === "files.download")!;
    const request = call.data as Record<string, unknown> & { url: string; names: string[] };
    expect(request.url.startsWith(`${BASE}/session-file?`)).toBe(true);
    expect(request.url).toContain("generation=g1");
    expect(request.url).toContain(`file_cap_get=${"a".repeat(128)}`);
    expect(request).toMatchObject({ directory: "folder-1", mimeType: "text/plain", expectedSize: 600 * 1024, maxBytes: 50 * 1024 * 1024, headers: { "Cache-Control": "no-store" } });
    expect(request.names.slice(0, 2)).toEqual(["notes.txt", "notes (1).txt"]);
  });

  test("a closed folder picker leaves nothing behind, and a failed download says why", async () => {
    let pick: { token: string } | null = null;
    const c = preview(meta(), {
      handlers: {
        "files.pickDirectory": () => pick,
        "files.download": () => {
          throw new Error("Session file download was truncated (expected 614400 bytes, received 10).");
        },
      },
      network: { [`POST ${BASE}/session-file-capability`]: capability() },
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:file-text");
    await app.press("id:file-download");
    await app.wait(50);
    expect(app.shows("id:file-download-status")).toBe(false);
    expect(app.host.calls().some((m) => m.name === "files.download")).toBe(false);
    pick = { token: "folder-1" };
    await app.press("id:file-download");
    await app.waitFor("id:file-download-status");
    expect(app.texts()).toContain("Download failed: Session file download was truncated (expected 614400 bytes, received 10).");
  });
});

describe("Brain workspace", () => {
  test("folders come first, each folder is read by its own path (the daemon lists one level), and a folder read once is not read again", async () => {
    const asked: Asked[] = [];
    // As daemon/brain/workspace.go answers: one level per path, `children` never filled.
    const folders: Record<string, unknown[]> = {
      "": [
        { name: "plan.md", path: "plan.md", kind: "file" },
        { name: "notes", path: "notes", kind: "directory" },
      ],
      notes: [
        { name: "today.txt", path: "notes/today.txt", kind: "file" },
        { name: "old", path: "notes/old", kind: "directory" },
      ],
      "notes/old": [],
    };
    const app = await open(HARNESS, {
      host: {
        params: { page: { name: "files-workspace" } },
        handlers: {
          ...secureStore(),
          "app.daemon": ({ type, body }: Asked) => {
            if (type.startsWith("brain_workspace")) asked.push({ type, body });
            if (type === "brain_workspace_tree") {
              const entries = folders[String(body.path)];
              if (!entries) return { error: "no such folder" };
              return { reply: "brain_workspace_tree", workspace_tree: { workspace: "/home/me/brain", path: body.path, entries } };
            }
            if (type === "brain_workspace_file") return { reply: "brain_workspace_file", file: { name: "today.txt", path: "notes/today.txt", kind: "file", language: "text", content: "ship it" } };
            return { error: `unexpected ${type}` };
          },
        },
      },
    });
    await app.waitFor("id:workspace-notes");
    expect(app.texts().indexOf("notes")).toBeLessThan(app.texts().indexOf("plan.md"));
    await app.press("id:workspace-notes");
    await app.waitFor("id:workspace-notes/today.txt");
    expect(app.shows("id:workspace-empty")).toBe(false);
    expect(asked.filter((a) => a.type === "brain_workspace_tree").map((a) => a.body)).toEqual([{ path: "" }, { path: "notes" }]);
    // A folder with nothing in it says so.
    await app.press("id:workspace-notes/old");
    await app.waitFor("id:workspace-empty");
    await app.back();
    await app.press("id:workspace-notes/today.txt");
    await app.waitFor("id:workspace-text");
    expect(app.texts()).toContain("ship it");
    expect(asked.at(-1)).toEqual({ type: "brain_workspace_file", body: { path: "notes/today.txt" } });
    // Back in a folder already read: no new request.
    await app.back();
    await app.back();
    await app.waitFor("id:workspace-notes");
    await app.press("id:workspace-notes");
    await app.waitFor("id:workspace-notes/today.txt");
    expect(asked.filter((a) => a.type === "brain_workspace_tree")).toHaveLength(3);
  });
});

type Upload = { uploadId: string; url: string; fileUri: string; expectedSize: number; headers: Record<string, string> };
type Answer = { status: number; body: string };

/**
 * A composer's uploads: the host plays the phone's picker and its streaming upload. A host
 * call left unanswered keeps the app from being quiet, which every wait of the test needs,
 * and a handler that touches the app while the test waits stalls; so each upload is answered
 * a moment later by itself, and `most` counts how many were ever in flight at once. Progress
 * events are core's to prove (core/files/uploads.test.ts, attachmentUploadPresentation.test.ts).
 */
function uploads(assets: unknown[], answer: (request: Upload, index: number) => Answer) {
  const started: Upload[] = [];
  const flight = { now: 0, most: 0 };
  return {
    started,
    flight,
    options: {
      host: {
        handlers: {
          ...secureStore(),
          "files.pick": () => assets,
          "files.upload": (request: Upload): Promise<Answer> => {
            const index = started.push(request) - 1;
            flight.now += 1;
            flight.most = Math.max(flight.most, flight.now);
            return Promise.resolve().then(() => {
              flight.now -= 1;
              return answer(request, index);
            });
          },
          "files.cancel": () => null,
        },
      },
    },
  };
}

const done = (path: string, name: string): Answer => ({ status: 200, body: JSON.stringify({ path, name }) });

const asset = (name: string, size = 1024) => ({ uri: `content://picker/${name}`, name, mimeType: "application/pdf", size });

describe("Attachments", () => {
  test("a picked file streams from its own URI to /upload, signed for upload, and is ready with the computer's path", async () => {
    const u = uploads([asset("報告 1.pdf")], () => done("/state/uploads/abc.pdf", "報告 1.pdf"));
    const app = await open(UPLOADS, u.options);
    await app.press("id:attach");
    await app.waitFor("text:Ready");
    expect(app.host.calls().find((m) => m.name === "files.pick")!.data).toEqual({ maxCount: 8 });
    const call = u.started[0]!;
    expect(call).toMatchObject({ url: `${BASE}/upload`, fileUri: "content://picker/報告 1.pdf", expectedSize: 1024 });
    expect(call.headers["Content-Type"]).toBe("application/pdf");
    expect(call.headers["X-Mewla-Upload-Name"]).toBe(encodeURIComponent("報告 1.pdf"));
    expect(call.headers.Authorization).toMatch(/v1:/);
    expect(app.texts()).toContain("/state/uploads/abc.pdf");
  });

  test("only one upload runs at a time; the next starts when it ends, and a refusal says the computer's words", async () => {
    const u = uploads([asset("a.pdf"), asset("b.pdf")], (_, index) => (index ? done("/state/uploads/b.pdf", "b.pdf") : { status: 507, body: "upload storage capacity reached\n" }));
    const app = await open(UPLOADS, u.options);
    await app.press("id:attach");
    await app.waitFor("text:Ready");
    // b.pdf waited for a.pdf's answer: never two in flight.
    expect(u.flight.most).toBe(1);
    expect(u.started.map((request) => request.fileUri)).toEqual(["content://picker/a.pdf", "content://picker/b.pdf"]);
    expect(app.texts()).toContain("upload storage capacity reached");
    expect(app.texts()).toContain("/state/uploads/b.pdf");
  });
});
