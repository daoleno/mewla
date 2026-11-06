import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { MessageBody } from "./InterfaceMessageBody";
import { InterfaceTimelineActivityDetails } from "./InterfaceTimelineActivityDetails";
import { InterfaceTimelineExpandedBlock } from "./InterfaceTimelineExpandedBlock";
import { InterfaceTimelineActivityHeader } from "./InterfaceTimelineActivityHeader";
import { useTimelineActivityExpansion } from "./InterfaceTimelineActivityExpansionState";
import {
  buildInterfaceTimelineActivityPresentation,
  shouldAutoExpandActivity,
} from "./InterfaceTimelineActivityModel";
import type {
  PatchFileSummary,
  ActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";

interface ActivityEventProps {
  item: ActivityTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function ActivityEvent({
  item,
  chrome,
  theme,
  loadAssetPreview: _loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: ActivityEventProps) {
  const defaultExpanded = shouldAutoExpandActivity(item);
  const { detailsExpanded, expanded, toggle } = useTimelineActivityExpansion(
    item.id,
    defaultExpanded,
  );
  const activityPresentation = buildInterfaceTimelineActivityPresentation(
    item,
    chrome,
  );


  if (item.activityKind === "update") {
    return (
      <View style={styles.wrap}>
        <MessageBody value={item.body ?? item.title} chrome={chrome} theme={theme} compact />
      </View>
    );
  }

  return (
    <View style={styles.wrap}>
      <InterfaceTimelineActivityHeader
        title={item.title}
        tone={item.tone}
        icon={item.icon}
        activityKind={item.activityKind}
        detail={item.detail}
        detailText={activityPresentation.detailText}
        statusMark={activityPresentation.statusMark}
        canExpand={activityPresentation.canExpand}
        expanded={expanded}
        toneColor={activityPresentation.toneColor}
        chrome={chrome}
        accessibilityLabel={item.accessibilityLabel}
        onPress={() => {
          if (activityPresentation.canExpand) {
            toggle();
          }
        }}
      />

      {detailsExpanded ? (
        item.steps?.length ? (
          // A folded run opens onto the real rows, each with its own disclosure.
          <InterfaceTimelineExpandedBlock chrome={chrome} style={styles.steps}>
            {item.steps.map((step) => (
              <ActivityEvent
                key={step.id}
                item={step}
                chrome={chrome}
                theme={theme}
                loadAssetPreview={_loadAssetPreview}
                formatPatchPath={formatPatchPath}
                truncateBody={truncateBody}
              />
            ))}
          </InterfaceTimelineExpandedBlock>
        ) : (
          <InterfaceTimelineActivityDetails
            item={item}
            chrome={chrome}
            theme={theme}
            assetPreviewUri={null}
            assetPreviewFailed={false}
            formatPatchPath={formatPatchPath}
            truncateBody={truncateBody}
          />
        )
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  // The rows inside a folded run sit tight, like rows on the timeline.
  steps: {
    gap: 0,
    paddingTop: 2,
    paddingBottom: 2,
  },
  // Tool rows are margin annotations: a compact, even rhythm between rows
  // and a little extra air where they meet prose (message spacing owns the
  // rest).
  wrap: {
    paddingVertical: 1,
    marginBottom: 3,
    paddingLeft: 1,
  },
});
