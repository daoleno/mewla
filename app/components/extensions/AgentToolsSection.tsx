import React from "react";
import { StyleSheet, View } from "react-native";
import { AgentLogoSet } from "../agents/AgentLogoSet";
import { AppText, EmptyState, ListRow, ListSection } from "../ui";
import { Icon } from "../icons/Icon";
import { useAppColors } from "../../constants/tokens";
import { skillsRequestData } from "../../services/skillsManagement";
import {
  agentExtensionsCountLabel,
  agentExtensionsSummaries,
  joinAgentLabels,
  type AgentExtensionsScope,
} from "../../services/agentExtensionsModel";
import type { AgentExtensions } from "./AgentExtensionsProvider";

/**
 * The agents' own plugins and Skills, below Mewla's: a quiet caption and one
 * row per tool that opens its management page. Shared Skills are listed once.
 */
export function AgentToolsSection({ agent, onOpen }: { agent: AgentExtensions; onOpen(scope: AgentExtensionsScope): void }) {
  const colors = useAppColors();
  const inventory = skillsRequestData(agent.inventoryState);
  const loading = !inventory && (agent.inventoryState.status === "idle" || agent.inventoryState.status === "loading");
  const failed = !inventory && agent.inventoryState.status === "error";
  const { rows, empty } = agentExtensionsSummaries(agent.listableSkills, agent.pluginCopies);
  // Offline, the Mewla plugins above already say so once for the page.
  if (!inventory && agent.connection !== "connected") return null;
  return (
    <View style={styles.section}>
      <View style={styles.caption}>
        <AppText variant="label" tone="tertiary" accessibilityRole="header">From your agents</AppText>
        <AppText variant="caption" tone="tertiary">Plugins and Skills that each tool installed itself. Manage or remove them here.</AppText>
      </View>
      {failed ? (
        <EmptyState icon="warning" title="Agent plugins and Skills unavailable" detail={agent.inventoryState.error} action={{ label: "Try again", icon: "refresh", onPress: () => { void agent.refreshInventory(); void agent.refreshPlugins(); } }} />
      ) : loading ? (
        <EmptyState busy title="Reading your agents" />
      ) : rows.length ? (
        <ListSection footer={empty.length ? `${joinAgentLabels(empty)} ${empty.length === 1 ? "has" : "have"} nothing installed.` : null}>
          {rows.map((row) => (
            <ListRow
              key={row.scope}
              leading={row.scope === "shared"
                ? <View style={[styles.sharedMark, { backgroundColor: colors.surfaceSubtle }]}><Icon name="swap-horizontal" size={16} color={colors.textSecondary} /></View>
                : <AgentLogoSet agents={[row.scope]} size={24} />}
              title={row.label}
              subtitle={row.scope === "shared"
                ? `${agentExtensionsCountLabel(row)} · used by ${joinAgentLabels(row.agents)}`
                : agentExtensionsCountLabel(row)}
              accessory="chevron"
              accessibilityHint={`Shows ${row.label === "Shared" ? "the shared Skills" : `${row.label}'s plugins and Skills`}`}
              onPress={() => onOpen(row.scope)}
            />
          ))}
        </ListSection>
      ) : (
        <AppText variant="caption" tone="tertiary" style={styles.none}>Your agents have no plugins or Skills of their own.</AppText>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { gap: 8 },
  caption: { paddingHorizontal: 4, gap: 2 },
  sharedMark: { width: 24, height: 24, borderRadius: 7, alignItems: "center", justifyContent: "center" },
  none: { paddingHorizontal: 4 },
});
