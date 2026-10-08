import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { InterfaceTimelineExpandedBlock } from "./InterfaceTimelineExpandedBlock";
import { InterfaceTimelinePlanExplanation } from "./InterfaceTimelinePlanExplanation";
import { PlanHeader } from "./InterfaceTimelinePlanHeader";
import { PlanSteps } from "./InterfaceTimelinePlanSteps";
import type { PlanTimelineItem } from "./InterfaceTimelinePlanTypes";

export function PlanUpdate({
  item,
  chrome,
  theme,
}: {
  item: PlanTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  return (
    <View style={styles.wrap}>
      <PlanHeader accentColor={chrome.textSubtle} chrome={chrome} />
      <InterfaceTimelineExpandedBlock chrome={chrome} style={styles.planBlock}>
        <InterfaceTimelinePlanExplanation
          chrome={chrome}
          explanation={item.explanation}
        />
        <PlanSteps steps={item.steps} chrome={chrome} theme={theme} />
      </InterfaceTimelineExpandedBlock>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    marginBottom: 8,
    paddingLeft: 1,
  },
  planBlock: {
    paddingVertical: 1,
  },
});
