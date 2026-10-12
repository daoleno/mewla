export function isTargetedBrainThreadReadOnly(
  targetedThreadId?: string,
  liveThreadId?: string,
): boolean {
  if (!targetedThreadId) return false;
  return !liveThreadId || targetedThreadId !== liveThreadId;
}

export interface BrainChatMount {
  serverId: string;
  threadScopeKey?: string;
  key: string;
}

/**
 * Brain's chat remounts only for another server or a switch between two known
 * threads. Before Brain's first snapshot the thread is unknown; the snapshot
 * that names it binds the mounted chat in place, so typing that started while
 * Brain woke keeps its draft, focus and scroll position.
 */
export function nextBrainChatMount(
  previous: BrainChatMount | null,
  serverId: string,
  threadScopeKey?: string,
): BrainChatMount {
  if (previous?.serverId === serverId) {
    if (!threadScopeKey || previous.threadScopeKey === threadScopeKey) {
      return previous;
    }
    if (!previous.threadScopeKey) {
      return { ...previous, threadScopeKey };
    }
  }
  return {
    serverId,
    threadScopeKey,
    key: `brain-chat:${serverId}:${threadScopeKey ?? ""}`,
  };
}
