import React from "react";
import {
  BrainWorkEventCard,
  type BrainWorkEventTimelineItem,
} from "../brain/BrainWorkEventCard";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ZenActivityEvent } from "./InterfaceTimelineActivity";
import type {
  PatchFileSummary,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import { ZenPlanUpdate } from "./InterfaceTimelinePlan";
import type { ZenPlanTimelineItem } from "./InterfaceTimelinePlanTypes";
import type { MessagePresentation } from "./InterfaceTimelineGrouping";
import { TaskNotificationCard } from "./TaskNotificationCard";
import {
  BRAIN_PRESENCE_ITEM_PREFIX,
  BrainCatRow,
  useBrainCompanion,
} from "../mewla/BrainCompanion";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "./InterfaceTimelineModel";
import type { TaskNotificationTimelineItem } from "./taskNotificationCardModel";
import {
  ZenAssistantMessage,
  ZenUserMessage,
  type ZenMessageTimelineItem,
} from "./InterfaceTimelineMessage";

export type ZenTimelineItem =
  | (ZenMessageTimelineItem & { role: "user" })
  | (ZenMessageTimelineItem & { role: "assistant" })
  | ZenActivityTimelineItem
  | ZenPlanTimelineItem
  | BrainWorkEventTimelineItem
  | TaskNotificationTimelineItem;

interface ZenTimelineItemViewProps {
  item: ZenTimelineItem;
  presentation?: MessagePresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

function ZenTimelineItemViewImpl({
  item,
  presentation,
  chrome,
  theme,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: ZenTimelineItemViewProps) {
  if (item.type === "message") {
    if (item.role === "user") {
      return (
        <ZenUserMessage
          item={item}
          presentation={presentation}
          chrome={chrome}
          theme={theme}
        />
      );
    }
    return (
      <ZenAssistantMessage
        item={item}
        presentation={presentation}
        chrome={chrome}
        theme={theme}
      />
    );
  }
  if (item.type === "plan") {
    return <ZenPlanUpdate item={item} chrome={chrome} theme={theme} />;
  }
  if (item.type === "brain-work-event") {
    return (
      <BrainWorkEventCard
        item={item}
        chrome={chrome}
      />
    );
  }
  if (item.type === "task-notification") {
    return (
      <TaskNotificationCard
        item={item}
        chrome={chrome}
        theme={theme}
      />
    );
  }
  const activity = (
    <ZenActivityEvent
      item={item}
      chrome={chrome}
      theme={theme}
      loadAssetPreview={loadAssetPreview}
      formatPatchPath={formatPatchPath}
      truncateBody={truncateBody}
    />
  );
  if (
    item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX) ||
    item.id.startsWith(BRAIN_PRESENCE_ITEM_PREFIX)
  ) {
    return <BrainActivityRow item={item} chrome={chrome} fallback={activity} />;
  }
  return activity;
}

/** Brain's Working and presence rows carry the cat; Sessions keep the row. */
function BrainActivityRow({
  item,
  chrome,
  fallback,
}: {
  item: ZenTimelineItem;
  chrome: TerminalThemeChrome;
  fallback: React.ReactElement;
}) {
  const companion = useBrainCompanion();
  if (!companion || item.type !== "activity") {
    return item.id.startsWith(BRAIN_PRESENCE_ITEM_PREFIX) ? null : fallback;
  }
  const working = item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX);
  return (
    <BrainCatRow
      companion={working ? { ...companion, presence: { state: "working" } } : companion}
      label={item.title}
      detail={item.detail}
      chrome={chrome}
      onPress={working ? undefined : companion.onOpenWork}
      turnRunning={working}
    />
  );
}

export const ZenTimelineItemView = React.memo(
  ZenTimelineItemViewImpl,
  areZenTimelineItemViewPropsEqual,
);

function areZenTimelineItemViewPropsEqual(
  previous: ZenTimelineItemViewProps,
  next: ZenTimelineItemViewProps,
) {
  return (
    previous.presentation === next.presentation &&
    previous.item === next.item &&
    previous.chrome === next.chrome &&
    previous.theme === next.theme &&
    previous.loadAssetPreview === next.loadAssetPreview &&
    previous.formatPatchPath === next.formatPatchPath &&
    previous.truncateBody === next.truncateBody
  );
}
