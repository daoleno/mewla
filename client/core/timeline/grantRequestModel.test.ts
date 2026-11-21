import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../conversation/conversation";
import { grantRequestTimelineItem, grantRequestView } from "./grantRequestModel";
import { buildTimeline } from "./model";

const event = (status: string, over: Partial<CodexConversationEvent> = {}): CodexConversationEvent => ({
  id: "grant:screen_recording:1", seq: 1, kind: "status", role: "assistant", status, source: "grant_request",
  grant_permission: "screen_recording", body: "A Worker needs to see this Mac's screen.", ...over,
});

describe("a grant request in Brain's thread", () => {
  test("is its own row, with the permission, the state and the reason", () => {
    expect(buildTimeline([event("asked")])).toEqual([
      { type: "grant-request", id: "grant:screen_recording:1", timestamp: undefined, permission: "screen_recording", state: "asked", reason: "A Worker needs to see this Mac's screen." },
    ]);
  });

  test("a status the app does not know, or no permission, stays the status line it was", () => {
    expect(grantRequestTimelineItem(event("later"))).toBeNull();
    expect(grantRequestTimelineItem(event("asked", { grant_permission: "microphone" }))).toBeNull();
    expect(buildTimeline([event("asked", { grant_permission: undefined })])[0]?.type).toBe("activity");
  });

  test("every state: what it offers and whether it is sealed", () => {
    const both = [{ id: "not_now", label: "Not now" }, { id: "open_settings", label: "Open System Settings" }];
    expect(grantRequestView({ state: "asked" })).toEqual({ sealed: false, buttons: both });
    // Settings opened: still the person's to give, so still both.
    expect(grantRequestView({ state: "opened" })).toEqual({ sealed: false, buttons: both });
    expect(grantRequestView({ state: "restart" })).toEqual({ sealed: false, buttons: [{ id: "restart", label: "Restart Mewla" }] });
    expect(grantRequestView({ state: "granted" })).toEqual({ sealed: true, word: "Allowed", buttons: [] });
    expect(grantRequestView({ state: "declined" })).toEqual({ sealed: true, word: "Not now", buttons: [] });
  });

  test("a new state under the same id replaces the row", () => {
    const asked = buildTimeline([event("asked")]);
    const granted = buildTimeline([event("granted")]);
    expect(asked[0]).not.toEqual(granted[0]);
  });
});
