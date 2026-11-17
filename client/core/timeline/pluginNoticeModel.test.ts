import { expect, test } from "bun:test";
import { buildTimeline } from "./model";
import type { CodexConversationEvent } from "../conversation/conversation";

test("an Extensions sign-in notice in Chat is its own row, naming the account its Sign in renews", () => {
  const notice: CodexConversationEvent = { id: "plugin-signin-lost:aws-cli-1:1", seq: 1, kind: "status", role: "assistant", title: "AWS sign-in ended", body: "AWS ended the sign-in for “prod”, so agents can't use it. Tap Sign in and approve the code on any device.", status: "warning", source: "plugin_notice", plugin_account_id: "aws-cli-1" };
  const items = buildTimeline([notice]);
  expect(items).toEqual([{ type: "plugin-notice", id: "plugin-signin-lost:aws-cli-1:1", timestamp: undefined, title: "AWS sign-in ended", body: notice.body!, accountId: "aws-cli-1" }]);
  // A notice from an older computer names no account: it stays the status line it was.
  const older = buildTimeline([{ ...notice, plugin_account_id: undefined }]);
  expect(older[0]?.type).toBe("activity");
});
