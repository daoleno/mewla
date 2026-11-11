import { effect, Text, untracked, View } from "nsh";
import { agentExtensionsCountLabel, agentExtensionsSummaries, joinAgentLabels } from "../../../../core/extensions/agentExtensionsModel";
import { skillsRequestData } from "../../../../core/extensions/skillsManagement";
import { daemon, daemonState } from "../../daemon";
import { Icon } from "../../kit/Icon";
import { EmptyState, Row } from "../../kit/kit";
import type { PageNav } from "../../routes";
import { AgentMark } from "./marks";
import { followComputer, listableSkills, pluginCopies, refreshAll, skillsState } from "./state";

/**
 * The agents' own plugins and Skills, below Mewla's: a quiet caption and one
 * row per tool that opens its page. Shared Skills are listed once.
 */
export function AgentToolsSection(props: { nav: PageNav }) {
  // Read on open and whenever the computer (re)connects.
  effect(() => {
    const current = daemon();
    followComputer(current?.server.id ?? "");
    if (current && daemonState() === "connected") untracked(refreshAll);
  });
  const inventory = () => skillsRequestData(skillsState());
  const summaries = () => agentExtensionsSummaries(listableSkills(), pluginCopies());
  // Offline, the Mewla plugins above already say so once for the page.
  const hidden = () => !inventory() && daemonState() !== "connected";
  const body = () => {
    const state = skillsState();
    if (!inventory() && state.status === "error") {
      return <EmptyState testID="agent-tools-failed" icon="warning" title="Agent plugins and Skills unavailable" detail={state.error} action="Try again" onAction={refreshAll} />;
    }
    if (!inventory()) return <EmptyState busy title="Reading your agents" />;
    const { rows, empty } = summaries();
    if (!rows.length) return <Text class="px-4 text-sm text-muted-foreground">Your agents have no plugins or Skills of their own.</Text>;
    return (
      <View>
        <View class="overflow-hidden rounded-2xl border border-border bg-card"><View class="-mb-px">
        {rows.map((row) => (
          <Row
            testID={`agent-tool-${row.scope}`}
            leading={row.scope === "shared" ? <View class="size-6 items-center justify-center rounded-md bg-secondary"><Icon name="swap-horizontal" size={16} /></View> : <AgentMark agent={row.scope} size={24} />}
            title={row.label}
            subtitle={row.scope === "shared" ? `${agentExtensionsCountLabel(row)} · used by ${joinAgentLabels(row.agents)}` : agentExtensionsCountLabel(row)}
            chevron
            accessibilityHint={`Shows ${row.label === "Shared" ? "the shared Skills" : `${row.label}'s plugins and Skills`}`}
            onPress={() => props.nav.push({ name: "extensions-agent", scope: row.scope })}
          />
        ))}
        </View></View>
        {empty.length ? <Text class="mt-1.5 px-4 text-xs text-muted-foreground">{`${joinAgentLabels(empty)} ${empty.length === 1 ? "has" : "have"} nothing installed.`}</Text> : null}
      </View>
    );
  };
  return (
    <View testID="agent-tools" class="mt-6 flex-col" style={{ display: hidden() ? "none" : "flex" }}>
      <View class="mb-1.5 gap-0.5 px-4">
        <Text class="text-sm font-medium text-muted-foreground" role="header">From your agents</Text>
        <Text class="text-xs text-muted-foreground">Plugins and Skills that each tool installed itself. Manage or remove them here.</Text>
      </View>
      {body()}
    </View>
  );
}
