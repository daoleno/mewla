import { describe, expect, test } from "bun:test";
import { normalizeCodexConversation } from "../../services/codexConversation";
import { buildTimeline } from "./InterfaceTimelineModel";

describe("provider update events", () => {
  test("a message and an update project to one message plus one quiet update line", () => {
    const { events } = normalizeCodexConversation({
      available: true,
      events: [
        { id: "u1", seq: 1, kind: "update", role: "assistant", body: "Quota is now 256." },
        { id: "m1", seq: 2, kind: "assistant_message", role: "assistant", body: "Done." },
      ],
    });
    const items = buildTimeline(events);
    expect(items.filter((item) => item.type === "message")).toHaveLength(1);
    const update = items.find((item) => item.type === "activity");
    expect(update).toMatchObject({ activityKind: "update", body: "Quota is now 256.", tone: "neutral" });
    expect(items).toHaveLength(2);
  });
});
