import { describe, expect, test } from "bun:test";
import {
  buildAttachmentEnvelope,
  splitAttachmentEnvelope,
  stripAttachmentEnvelope,
} from "./attachmentEnvelope";

describe("attachment envelope", () => {
  test("round-trips the files a message carries", () => {
    const envelope = buildAttachmentEnvelope([
      { name: "photo.png", path: "/uploads/photo.png", mimeType: "image/png" },
      { name: "notes.txt", path: "/uploads/notes.txt" },
    ]);
    const split = splitAttachmentEnvelope(`look\n\n${envelope}`);
    expect(split.text).toBe("look\n\n");
    expect(split.attachments).toEqual([
      { name: "photo.png", path: "/uploads/photo.png", mimeType: "image/png" },
      { name: "notes.txt", path: "/uploads/notes.txt", mimeType: undefined },
    ]);
    expect(stripAttachmentEnvelope(`look ${envelope}`)).toBe("look ");
  });

  test("reads the daemon's loose spacing and ignores entries without a path", () => {
    const split = splitAttachmentEnvelope(
      'a <mewla_attachments>\n {"files":[{"name":"x"},{"name":"y","path":" /p "}]}\n</mewla_attachments>',
    );
    expect(split.attachments).toEqual([
      { name: "y", path: "/p", mimeType: undefined },
    ]);
  });

  test("treats plain and malformed text as having no attachments", () => {
    expect(splitAttachmentEnvelope("plain")).toEqual({ text: "plain", attachments: [] });
    expect(
      splitAttachmentEnvelope("a <mewla_attachments>{broken</mewla_attachments>").attachments,
    ).toEqual([]);
    // The retired "Uploaded files:" list is plain text now.
    const legacy = "hi\nUploaded files:\n- a.png: /uploads/a.png";
    expect(splitAttachmentEnvelope(legacy)).toEqual({ text: legacy, attachments: [] });
  });
});
