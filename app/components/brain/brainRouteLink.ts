export interface BrainRouteLink {
  serverId?: string;
  brainThreadId?: string;
  brainMessageId?: string;
}

/** One notification, as the route carries it: the computer, thread and message. */
export function brainLinkKey(link: BrainRouteLink): string {
  const serverId = link.serverId?.trim() || "";
  return serverId ? [serverId, link.brainThreadId ?? "", link.brainMessageId ?? ""].join("|") : "";
}

/**
 * What Brain does with a notification's route. The link's computer is applied
 * once: after this page has shown it, switching computer from the menu or
 * Settings is not undone by the same link, and a new notification (another
 * thread or message) applies again. A thread only ever opens on the computer
 * it was sent from.
 */
export function brainRouteLink(
  link: BrainRouteLink,
  activeServerId: string | null | undefined,
  shownKey: string,
): {
  /** The computer to switch to; empty when there is none to apply. */
  switchTo: string;
  /** Whether the page shows the current computer's own Brain, not one still being switched to. */
  showsCurrent: boolean;
  /** The thread to open, only on the computer the link was sent from. */
  threadId: string | undefined;
  /** The key to remember once the link's computer is current. */
  shown: string;
} {
  const linkedServerId = link.serverId?.trim() || "";
  const key = brainLinkKey(link);
  const switchTo = key && key === shownKey ? "" : linkedServerId;
  const onLinkedComputer = !linkedServerId || activeServerId === linkedServerId;
  return {
    switchTo,
    showsCurrent: !switchTo || activeServerId === switchTo,
    threadId: onLinkedComputer ? link.brainThreadId : undefined,
    shown: key && activeServerId === linkedServerId ? key : shownKey,
  };
}
