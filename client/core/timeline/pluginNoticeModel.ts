import type { CodexConversationEvent } from "../conversation/conversation";

/**
 * A line Mewla writes in Chat when an Extensions account's sign-in ends or is about to (an AWS profile, Slack):
 * what happened, and a Sign in that starts that account's own sign-in (a device code for AWS Identity Center).
 */
export type PluginNoticeTimelineItem = {
  type: "plugin-notice";
  id: string;
  timestamp?: string;
  title: string;
  body: string;
  accountId: string;
};

export const PLUGIN_NOTICE_SOURCE = "plugin_notice";

export function pluginNoticeTimelineItem(event: CodexConversationEvent): PluginNoticeTimelineItem | null {
  const accountId = (event.plugin_account_id || "").trim();
  if (event.kind !== "status" || event.source !== PLUGIN_NOTICE_SOURCE || !accountId) return null;
  return {
    type: "plugin-notice",
    id: event.id || `plugin-notice:${event.seq}`,
    timestamp: event.timestamp,
    title: (event.title || "").trim(),
    body: (event.body || "").trim(),
    accountId,
  };
}
