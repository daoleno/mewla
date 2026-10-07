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
 *   (when its slip is not in this conversation), a result, or Workers.
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
  const perch = presence.state === "attention" ? perchIndex(items, presence.workId) : -1;
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
      title: label,
      detail: presence.workTitle,
      tone: "neutral",
      icon: "paw-outline",
      statusKey: presence.state,
      defaultExpanded: false,
    },
  ];
}

/** The newest slip for the Work that needs you, or -1. */
function perchIndex(items: ZenTimelineItem[], workId: string | undefined): number {
  if (!workId) return -1;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.type === "brain-work-event" && item.event.work_id === workId) return index;
  }
  return -1;
}
