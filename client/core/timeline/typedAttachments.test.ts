import { describe, expect, test } from "bun:test";
import { normalizeCodexConversation } from "../conversation/conversation";
import { buildTimeline } from "./model";

describe("typed attachments", () => {
  test("the timeline renders attachment chips from the event field", () => {
    const conversation = normalizeCodexConversation({
      available: true,
      events: [
        {
          id: "u1",
          seq: 1,
          kind: "user_message",
          role: "user",
          body: "look at this",
          attachments: [
            { name: "photo.png", path: "/uploads/photo.png", content_type: "image/png" },
            { name: "no path" },
          ],
        },
        { id: "u2", seq: 2, kind: "user_message", role: "user", attachments: [{ name: "n.txt", path: "/uploads/n.txt" }] },
      ],
    });
    const items = buildTimeline(conversation.events);
    expect(items).toHaveLength(2);
    expect(items[0]).toMatchObject({
      type: "message",
      body: "look at this",
      attachments: [{ name: "photo.png", path: "/uploads/photo.png", mimeType: "image/png" }],
    });
    expect(items[1]).toMatchObject({
      body: "",
      attachments: [{ name: "n.txt", path: "/uploads/n.txt", mimeType: undefined }],
    });
  });

  test("a stored row whose text still holds the old tag is plain text", () => {
    const body =
      'look <mewla_attachments>{"files":[{"name":"x","path":"/uploads/x.png"}]}</mewla_attachments>';
    const [item] = buildTimeline(
      normalizeCodexConversation({
        available: true,
        events: [{ id: "old", seq: 1, kind: "user_message", role: "user", body }],
      }).events,
    );
    expect(item).toMatchObject({ type: "message", body, attachments: [] });
  });
});
