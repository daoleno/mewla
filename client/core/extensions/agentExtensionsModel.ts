import type { InstalledPluginCopy, PluginHost } from "./pluginsManagement";
import { groupLogicalPlugins, type LogicalPlugin } from "./pluginsScreenModel";
import type { InstalledSkill, ManagedSkillAgent } from "./skillsManagement";
import { skillAgentLabel } from "./skillsManagement";
import { groupLogicalSkills, MANAGED_SKILL_AGENTS, type LogicalSkill } from "./skillsScreenModel";

/**
 * Where an agent's own plugin or Skill is listed on Extensions: under the one
 * tool it belongs to, or once under Shared when a copy serves several tools
 * (~/.agents/skills) or none.
 */
export type AgentExtensionsScope = ManagedSkillAgent | "shared";

const PLUGIN_HOST_AGENT: Record<PluginHost, ManagedSkillAgent> = {
  codex: "codex",
  claude: "claude-code",
};

export function isAgentExtensionsScope(value: string | undefined): value is AgentExtensionsScope {
  return value === "shared" || MANAGED_SKILL_AGENTS.includes(value as ManagedSkillAgent);
}

export function agentExtensionsScopeLabel(scope: AgentExtensionsScope): string {
  return scope === "shared" ? "Shared" : skillAgentLabel(scope);
}

/** The scope one Skill copy is listed under. */
export function skillCopyScope(copy: InstalledSkill): AgentExtensionsScope {
  return copy.agents.length === 1 ? copy.agents[0]! : "shared";
}

/** The Skills listed under one scope, grouped by name like the rest of the app. */
export function skillsForScope(
  copies: readonly InstalledSkill[],
  scope: AgentExtensionsScope,
): LogicalSkill[] {
  return groupLogicalSkills(copies.filter((copy) => skillCopyScope(copy) === scope));
}

/** The plugins one tool installed. Plugins always belong to exactly one host. */
export function pluginsForScope(
  copies: readonly InstalledPluginCopy[],
  scope: AgentExtensionsScope,
): LogicalPlugin[] {
  if (scope === "shared") return [];
  return groupLogicalPlugins(copies.filter((copy) => PLUGIN_HOST_AGENT[copy.host] === scope));
}

export interface AgentExtensionsSummary {
  scope: AgentExtensionsScope;
  label: string;
  plugins: number;
  skills: number;
  /** For Shared: the tools its Skills serve. */
  agents: ManagedSkillAgent[];
}

/**
 * One row per tool with something installed, in the app's tool order, then
 * Shared. Counts are logical items, as each tool's page lists them.
 */
export function agentExtensionsSummaries(
  skills: readonly InstalledSkill[],
  plugins: readonly InstalledPluginCopy[],
): { rows: AgentExtensionsSummary[]; empty: ManagedSkillAgent[] } {
  const rows: AgentExtensionsSummary[] = [];
  const empty: ManagedSkillAgent[] = [];
  for (const agent of MANAGED_SKILL_AGENTS) {
    const row = {
      scope: agent,
      label: skillAgentLabel(agent),
      plugins: pluginsForScope(plugins, agent).length,
      skills: skillsForScope(skills, agent).length,
      agents: [agent],
    };
    if (row.plugins || row.skills) rows.push(row);
    else empty.push(agent);
  }
  const shared = skillsForScope(skills, "shared");
  if (shared.length) {
    rows.push({
      scope: "shared",
      label: "Shared",
      plugins: 0,
      skills: shared.length,
      agents: MANAGED_SKILL_AGENTS.filter((agent) => shared.some((skill) => skill.agents.includes(agent))),
    });
  }
  return { rows, empty };
}

export function agentExtensionsCountLabel(summary: Pick<AgentExtensionsSummary, "plugins" | "skills">): string {
  return [
    summary.plugins ? `${summary.plugins} ${summary.plugins === 1 ? "plugin" : "plugins"}` : "",
    summary.skills ? `${summary.skills} ${summary.skills === 1 ? "Skill" : "Skills"}` : "",
  ].filter(Boolean).join(" · ");
}

/** "Codex, Claude Code and Pi" */
export function joinAgentLabels(agents: readonly ManagedSkillAgent[]): string {
  const labels = agents.map(skillAgentLabel);
  if (labels.length <= 1) return labels[0] ?? "";
  return `${labels.slice(0, -1).join(", ")} and ${labels.at(-1)}`;
}
