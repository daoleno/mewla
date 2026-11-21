import type { CodexConversationEvent } from "../conversation/conversation";

/**
 * A request in Brain's thread for a grant on this Mac that a Worker needs and only the person can give in System
 * Settings (DESIGN-MAC.md §2.4, `m-perm`): the reason in one line, and Not now / Open System Settings. It seals when the
 * grant counts (or when the person says Not now); an Accessibility grant that does not count in the running Mewla
 * yet ends in one button that restarts it.
 */
export type GrantPermission = "screen_recording" | "accessibility" | "chrome";

/** asked → opened (System Settings shown) → granted; restart: granted but not yet in effect; declined: Not now. */
export type GrantRequestState = "asked" | "opened" | "restart" | "granted" | "declined";

export type GrantRequestTimelineItem = {
  type: "grant-request";
  id: string;
  timestamp?: string;
  permission: GrantPermission;
  state: GrantRequestState;
  /** What the grant is for, in one line. */
  reason: string;
};

export const GRANT_REQUEST_SOURCE = "grant_request";

const PERMISSIONS: readonly string[] = ["screen_recording", "accessibility", "chrome"];
const STATES: readonly string[] = ["asked", "opened", "restart", "granted", "declined"];

export function grantRequestTimelineItem(event: CodexConversationEvent): GrantRequestTimelineItem | null {
  const permission = (event.grant_permission || "").trim();
  const state = (event.status || "").trim();
  if (event.kind !== "status" || event.source !== GRANT_REQUEST_SOURCE || !PERMISSIONS.includes(permission) || !STATES.includes(state)) return null;
  return {
    type: "grant-request",
    id: event.id || `grant-request:${event.seq}`,
    timestamp: event.timestamp,
    permission: permission as GrantPermission,
    state: state as GrantRequestState,
    reason: (event.body || "").trim(),
  };
}

/** What a card shows and offers in a state: the buttons by id, and the word a sealed card ends in. */
export interface GrantRequestView {
  sealed: boolean;
  /** The sealed state's word: Allowed, Not now. */
  word?: string;
  /** In order. "open" is Open System Settings. */
  buttons: ReadonlyArray<{ id: "not_now" | "open_settings" | "restart"; label: string }>;
}

export function grantRequestView(item: Pick<GrantRequestTimelineItem, "state">): GrantRequestView {
  switch (item.state) {
    case "granted":
      return { sealed: true, word: "Allowed", buttons: [] };
    case "declined":
      return { sealed: true, word: "Not now", buttons: [] };
    case "restart":
      return { sealed: false, buttons: [{ id: "restart", label: "Restart Mewla" }] };
    default:
      // Asked, and System Settings opened: the same two buttons (the grant is still the person's to give).
      return { sealed: false, buttons: [{ id: "not_now", label: "Not now" }, { id: "open_settings", label: "Open System Settings" }] };
  }
}

/** The pane's name for the card's accessibility label. */
export function grantPermissionName(permission: GrantPermission): string {
  switch (permission) {
    case "screen_recording":
      return "Screen recording";
    case "accessibility":
      return "Accessibility";
    case "chrome":
      return "Chrome";
  }
}

/** The card's title, as `m-perm` words it: "Allow screen recording". Also the tray row's words. */
export function grantTitle(permission: GrantPermission): string {
  switch (permission) {
    case "screen_recording":
      return "Allow screen recording";
    case "accessibility":
      return "Allow Accessibility";
    case "chrome":
      return "Allow Chrome";
  }
}
