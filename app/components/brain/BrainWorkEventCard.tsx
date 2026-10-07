import React, { useMemo } from "react";
import { StyleSheet, Text, View } from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import { useBrainCompanion } from "../mewla/BrainCompanion";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import { brainWorkEventCardModel } from "./brainWorkEventCardModel";
import { BRAIN_WORK_CARD_FACT_LINES } from "./brainWorkEventCardLayout";
import {
  brainWorkEventAccessibilityLabel,
  brainCurrentWorkLifecycle,
  brainWorkEventLifecycle,
  brainWorkLifecycleStatus,
  brainWorkEventWorkTitle,
} from "./brainWorkEventPresentation";
import { WorkSlip } from "./WorkSlip";

export { chromeStatusInks } from "./WorkSlip";

export type BrainWorkEventTimelineItem = {
  type: "brain-work-event";
  id: string;
  timestamp: string;
  event: BrainWorkResultEvent;
  events: BrainWorkResultEvent[];
  sourceCount?: number;
  currentWork?: import("../../store/brain").BrainCurrentWork;
  onPress?: () => void;
  /** Brain only: the cat sits on this slip because its Work needs you. */
  catPerched?: boolean;
};

/**
 * A Work result in the Brain conversation, at the moment it came back. Every
 * slip has the same anatomy (who · when, state, title, one line), so a bare
 * lifecycle result still reads as Work rather than a status row; it simply
 * has no summary line.
 */
export function BrainWorkEventCard({
  item,
  chrome,
}: {
  item: BrainWorkEventTimelineItem;
  chrome: TerminalThemeChrome;
}) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const companion = useBrainCompanion();
  // While the Work is current, the slip carries its next step inline; a tap
  // still opens this result, so you read what came back before accepting it.
  const live = companion?.workSlip?.(item.event.work_id);
  const presentation = item.currentWork
    ? brainCurrentWorkLifecycle(item.currentWork, item.event)
    : brainWorkEventLifecycle(item.event);
  const status = brainWorkLifecycleStatus(presentation.lifecycle);
  const workTitle = brainWorkEventWorkTitle(item.event);
  const card = brainWorkEventCardModel(item.event);
  const time = formatChatBubbleTime(item.event.occurred_at);
  const who = item.event.session_id
    ? companion?.sessionLabels?.get(item.event.session_id)
    : undefined;
  const accessibilityLabel = brainWorkEventAccessibilityLabel({
    event: item.event,
    statusLabel: presentation.label,
    occurredAtLabel: new Date(item.event.occurred_at).toLocaleString(),
    description: [card.summary, ...card.facts].filter(
      (value): value is string => Boolean(value),
    ),
  });

  return (
    <WorkSlip
      chrome={chrome}
      status={status}
      statusLabel={presentation.label}
      title={workTitle}
      meta={`${who || "Work"} · ${time}`}
      summary={card.density === "minimal" ? undefined : card.summary}
      unread={item.event.unread}
      perched={Boolean(item.catPerched && companion)}
      animate={companion?.animate}
      actions={live?.actions}
      onPress={item.onPress}
      accessibilityLabel={accessibilityLabel}
    >
      {card.facts.length > 0 ? (
        <View style={styles.facts}>
          {card.facts.map((fact) => (
            <Text key={fact} numberOfLines={BRAIN_WORK_CARD_FACT_LINES} style={styles.fact}>
              {fact}
            </Text>
          ))}
        </View>
      ) : null}
    </WorkSlip>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    facts: {
      gap: 3,
      marginTop: 8,
    },
    fact: {
      ...TypeScale.caption,
      color: chrome.textMuted,
    },
  });
}
