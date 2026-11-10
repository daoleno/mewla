/**
 * Brain's "Worked" fold, from the Expo app's components/mewla/brainPresenceTimeline.ts.
 * The cat companion's `mergeBrainPresenceIntoTimeline` stays in the app: it needs the
 * cat's presence state and labels (`brainCatState`); only its row id prefix is kept,
 * because the fold must leave the cat's tail row alone.
 */
import type { ActivityTimelineItem } from "./activityTypes";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "./model";
import type { TimelineItem } from "./types";

/** Timeline id prefix for the tail row the cat holds between turns. */
export const BRAIN_PRESENCE_ITEM_PREFIX = "brain-presence:";

/** Timeline id prefix for a folded run of Brain's tool rows. */
export const BRAIN_STEPS_ITEM_PREFIX = "brain-steps:";

/**
 * Brain only: a turn's tool rows (Search, Read, Run …) fold into one quiet
 * "Worked · N steps" row that expands to the steps, so the conversation
 * reads as messages and Work slips. A single row stays as it is, and a
 * still-running row stays visible on its own.
 */
export function foldBrainToolRows(items: TimelineItem[]): TimelineItem[] {
  const out: TimelineItem[] = [];
  let run: ActivityTimelineItem[] = [];
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

function isFoldableToolRow(item: TimelineItem): item is ActivityTimelineItem {
  return (
    item.type === "activity" &&
    item.tone !== "running" &&
    !item.streaming &&
    // Brain's own words between steps stay visible, never folded into steps.
    item.activityKind !== "update" &&
    !item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX) &&
    !item.id.startsWith(BRAIN_PRESENCE_ITEM_PREFIX) &&
    // Your answer to Brain's question is part of the conversation, not a step.
    !item.statusKey?.startsWith("choice:")
  );
}

function foldedSteps(run: ActivityTimelineItem[]): ActivityTimelineItem {
  const failed = run.filter((item) => item.tone === "failed").length;
  const title = `Worked · ${run.length} steps${failed ? ` · ${failed} failed` : ""}`;
  return {
    type: "activity",
    id: `${BRAIN_STEPS_ITEM_PREFIX}${run[0].id}`,
    timestamp: run[run.length - 1].timestamp ?? run[0].timestamp,
    statusKey: failed ? "failed" : "done",
    title,
    tone: failed ? "failed" : "neutral",
    icon: failed ? "alert-circle" : "layers",
    defaultExpanded: false,
    accessibilityLabel: title,
    children: run.flatMap((item) =>
      item.children?.length
        ? item.children
        : [{ id: item.id, title: item.title, tone: item.tone, providerToolId: item.providerToolId }],
    ),
  };
}
