import { describe, expect, test } from "bun:test";
import { firstMessageNews } from "./firstMessage";

describe("session_first_message (WIRE §11)", () => {
  test("reads the three outcomes, with the text that came back", () => {
    expect(firstMessageNews({ type: "session_first_message", worker_id: "w1", outcome: "sent" })).toEqual({ workerId: "w1", outcome: "sent", text: "" });
    expect(firstMessageNews({ type: "session_first_message", worker_id: "w1", outcome: "not_sent", text: "Fix it" })).toEqual({ workerId: "w1", outcome: "not_sent", text: "Fix it" });
    expect(firstMessageNews({ type: "session_first_message", worker_id: "w1", outcome: "unknown", text: "Fix it" })?.outcome).toBe("unknown");
  });

  test("drops a frame without a Session or with an outcome it does not know", () => {
    expect(firstMessageNews({ type: "session_first_message", outcome: "sent" })).toBeNull();
    expect(firstMessageNews({ type: "session_first_message", worker_id: "w1", outcome: "queued" })).toBeNull();
  });
});
