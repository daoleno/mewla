import { effect, Text, untracked, View } from "nsh";
import { agentExtensionsCountLabel, agentExtensionsSummaries, joinAgentLabels } from "../../../../core/extensions/agentExtensionsModel";
import { skillsRequestData } from "../../../../core/extensions/skillsManagement";
import { daemon, daemonState } from "../../daemon";
import { Icon } from "../../../kit/Icon";
import { EmptyState, Row, Section, Tile } from "../../../kit/kit";
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
    if (!rows.length) return <Section title="From your agents"><Row title="Nothing installed" /></Section>;
    return (
      <View>
        <Section title="From your agents">
        {rows.map((row) => (
          <Row
            testID={`agent-tool-${row.scope}`}
            leading={<Tile>{row.scope === "shared" ? <Icon name="swap-horizontal" size={20} /> : <AgentMark agent={row.scope} size={20} />}</Tile>}
            title={row.label}
            subtitle={row.scope === "shared" ? `${agentExtensionsCountLabel(row)} · used by ${joinAgentLabels(row.agents)}` : agentExtensionsCountLabel(row)}
            chevron
            accessibilityHint={`Shows ${row.label === "Shared" ? "the shared Skills" : `${row.label}'s plugins and Skills`}`}
            onPress={() => props.nav.push({ name: "extensions-agent", scope: row.scope })}
          />
        ))}
        </Section>
        {empty.length ? <Text class="mt-2 px-4 text-caption text-muted-foreground">{`${joinAgentLabels(empty)} ${empty.length === 1 ? "has" : "have"} nothing installed.`}</Text> : null}
      </View>
    );
  };
  return (
    <View testID="agent-tools" class="flex-col" style={{ display: hidden() ? "none" : "flex" }}>
      {body()}
    </View>
  );
}
