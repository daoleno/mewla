import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "../terminal/InterfaceTimelineModel";
import { brainCatTailLabel, type BrainCatPresence } from "./brainCatState";

/** Timeline id prefix for the tail row the cat holds between turns. */
export const BRAIN_PRESENCE_ITEM_PREFIX = "brain-presence:";

/**
 * Between turns, the cat holds the newest edge of a Brain conversation to say
 * what is waiting: your input, a result, or Workers. A running turn's Working
 * row already carries it, and an empty chat has the seal instead.
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
