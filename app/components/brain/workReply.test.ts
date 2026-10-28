import { describe, expect, test } from "bun:test";
import { workReplyFiles, workReplyReady } from "./workReply";

const ready = { id: "a", name: "shot.png", path: "/uploads/shot.png", mimeType: "image/png", uploadStatus: "ready" as const };
const uploading = { id: "b", name: "big.zip", path: "", uploadStatus: "uploading" as const };
const failed = { id: "c", name: "bad.pdf", path: "", uploadStatus: "failed" as const };

describe("work reply attachments", () => {
  test("only uploaded files are sent, with their type", () => {
    expect(workReplyFiles([ready, uploading, failed])).toEqual([
      { name: "shot.png", path: "/uploads/shot.png", content_type: "image/png" },
    ]);
    expect(workReplyFiles([{ id: "d", name: "n.txt", path: "/uploads/n.txt" }])).toEqual([
      { name: "n.txt", path: "/uploads/n.txt" },
    ]);
  });

  test("words alone, files alone, or both can be sent; nothing cannot", () => {
    expect(workReplyReady("keep both", [], false)).toBe(true);
    expect(workReplyReady("  ", [ready], false)).toBe(true);
    expect(workReplyReady("look", [ready], false)).toBe(true);
    expect(workReplyReady("  ", [], false)).toBe(false);
  });

  test("an upload in progress or failed blocks the reply, and so does a reply in flight", () => {
    expect(workReplyReady("look", [ready, uploading], false)).toBe(false);
    expect(workReplyReady("look", [ready, failed], false)).toBe(false);
    expect(workReplyReady("look", [ready], true)).toBe(false);
  });
});
