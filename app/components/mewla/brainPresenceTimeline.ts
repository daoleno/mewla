import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import type { ZenActivityTimelineItem } from "../terminal/InterfaceTimelineActivityTypes";
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

/** Timeline id prefix for a folded run of Brain's tool rows. */
export const BRAIN_STEPS_ITEM_PREFIX = "brain-steps:";

/**
 * Brain only: a turn's tool rows (Search, Read, Run …) fold into one quiet
 * "Worked · N steps" row that expands to the steps, so the conversation
 * reads as messages and Work slips. A single row stays as it is, and a
 * still-running row stays visible on its own.
 */
export function foldBrainToolRows(items: ZenTimelineItem[]): ZenTimelineItem[] {
  const out: ZenTimelineItem[] = [];
  let run: ZenActivityTimelineItem[] = [];
  const flush = () => {
    if (run.length > 1) out.push(foldedSteps(run));
    else out.push(...run);
    run = [];
  };
  for (const item of items) {
    if (isFoldableToolRow(item)) {
      run.push(item);
      continue;
    }
    flush();
    out.push(item);
  }
  flush();
  return out.length === items.length ? items : out;
}

function isFoldableToolRow(item: ZenTimelineItem): item is ZenActivityTimelineItem {
  return (
    item.type === "activity" &&
    item.tone !== "running" &&
    !item.streaming &&
    !item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX) &&
    !item.id.startsWith(BRAIN_PRESENCE_ITEM_PREFIX)
  );
}

function foldedSteps(run: ZenActivityTimelineItem[]): ZenActivityTimelineItem {
  const failed = run.filter((item) => item.tone === "failed").length;
  const title = `Worked · ${run.length} steps${failed ? ` · ${failed} failed` : ""}`;
  return {
    type: "activity",
    id: `${BRAIN_STEPS_ITEM_PREFIX}${run[0].id}`,
    timestamp: run[run.length - 1].timestamp ?? run[0].timestamp,
    statusKey: failed ? "failed" : "done",
    title,
    tone: failed ? "failed" : "neutral",
    icon: failed ? "alert-circle-outline" : "layers-outline",
    defaultExpanded: false,
    accessibilityLabel: title,
    children: run.flatMap((item) =>
      item.children?.length
        ? item.children
        : [{ id: item.id, title: item.title, tone: item.tone, providerToolId: item.providerToolId }],
    ),
  };
}
