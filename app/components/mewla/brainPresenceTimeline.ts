import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "../terminal/InterfaceTimelineModel";
import type { BrainWorkEventTimelineItem } from "../brain/BrainWorkEventCard";
import { brainCatTailLabel, type BrainCatPresence } from "./brainCatState";

/** Timeline id prefix for the tail row the cat holds between turns. */
export const BRAIN_PRESENCE_ITEM_PREFIX = "brain-presence:";

/**
 * The one cat, placed between turns:
 * - a running turn's Working row already carries it (walking);
 * - when Work needs you, it hops onto that Work's newest slip;
 * - otherwise it holds the newest edge to say what is waiting: your input
 *   (when no such slip is in this conversation), results, or Workers, as a
 *   count; tapping it opens the Work list.
 * An empty chat has the seal instead. Never more than one of these.
 */
export function mergeBrainPresenceIntoTimeline(
  items: ZenTimelineItem[],
  presence: BrainCatPresence | undefined,
): ZenTimelineItem[] {
  const label = presence ? brainCatTailLabel(presence) : null;
  if (
    !presence ||
    !label ||
    items.length === 0 ||
    items.some((item) => item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX))
  ) {
    return items;
  }
  const perch = presence.state === "attention" ? perchIndex(items, presence.workIds) : -1;
  if (perch >= 0) {
    const next = items.slice();
    next[perch] = { ...(items[perch] as BrainWorkEventTimelineItem), catPerched: true };
    return next;
  }
  return [
    ...items,
    {
      type: "activity",
      id: `${BRAIN_PRESENCE_ITEM_PREFIX}${presence.state}`,
      // Presence only: one Work's title here went stale; the Work list names them.
      title: label,
      tone: "neutral",
      icon: "paw-outline",
      statusKey: presence.state,
      defaultExpanded: false,
    },
  ];
}

/** The newest slip of any Work that needs you, or -1. */
function perchIndex(items: ZenTimelineItem[], workIds: readonly string[] | undefined): number {
  if (!workIds?.length) return -1;
  const needs = new Set(workIds);
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.type === "brain-work-event" && needs.has(item.event.work_id)) return index;
  }
  return -1;
}
