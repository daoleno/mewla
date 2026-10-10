import React, { useMemo, useState } from "react";
import { Stack, useLocalSearchParams, useRouter } from "expo-router";
import { SkillsPresentation } from "../../../components/skills/SkillsPresentation";
import { useAgentExtensions } from "../../../components/extensions/AgentExtensionsProvider";
import {
  agentExtensionsScopeLabel,
  isAgentExtensionsScope,
  pluginsForScope,
  skillCopyScope,
  skillsForScope,
} from "../../../services/agentExtensionsModel";
import { MANAGED_SKILL_AGENTS } from "../../../services/skillsScreenModel";
import type { SkillsSurfaceSection } from "../../../services/skillsSurfaceModel";

/** One tool's own plugins and Skills, or the Skills several tools share. */
export default function AgentExtensionsScreen() {
  const { tool } = useLocalSearchParams<{ tool: string }>();
  const scope = isAgentExtensionsScope(tool) ? tool : "shared";
  const router = useRouter();
  const agent = useAgentExtensions();
  const plugins = useMemo(() => pluginsForScope(agent.pluginCopies, scope), [agent.pluginCopies, scope]);
  const skills = useMemo(() => skillsForScope(agent.listableSkills, scope), [agent.listableSkills, scope]);
  const pluginOwnedSkillCount = useMemo(
    () => agent.skills.filter((copy) => skillCopyScope(copy) === scope).length
      - agent.listableSkills.filter((copy) => skillCopyScope(copy) === scope).length,
    [agent.listableSkills, agent.skills, scope],
  );
  const sections: SkillsSurfaceSection[] = scope === "codex" || scope === "claude-code" ? ["plugins", "skills"] : ["skills"];
  const [chosen, setChosen] = useState<SkillsSurfaceSection | null>(null);
  const [focusedPluginKey, setFocusedPluginKey] = useState<string | null>(null);
  // A tool opens on its plugins while it has any, otherwise on its Skills.
  const section = chosen ?? (sections.includes("plugins") && plugins.length ? "plugins" : "skills");
  const filterAgents = scope === "shared"
    ? MANAGED_SKILL_AGENTS.filter((item) => skills.some((skill) => skill.agents.includes(item)))
    : [];
  return (
    <>
      <Stack.Screen options={{ title: agentExtensionsScopeLabel(scope) }} />
      <SkillsPresentation
        section={section}
        sections={sections}
        filterAgents={filterAgents}
        inventoryState={agent.inventoryState}
        logicalSkills={skills}
        pluginsState={agent.pluginsState}
        logicalPlugins={plugins}
        skills={agent.skills}
        mutationOperations={agent.mutationOperations}
        preparingMutation={agent.preparingMutation}
        mutationNotice={agent.notice}
        currentServerAvailable={agent.currentServerAvailable}
        serverName={agent.serverName}
        connection={agent.connection}
        projectCwd={agent.projectCwd}
        inspectedName={agent.inspectedName}
        inspectedCopyId={agent.inspectedCopyId}
        inspectState={agent.inspectState}
        pluginOwnedSkillCount={pluginOwnedSkillCount}
        onSelectSection={setChosen}
        onOpenSettings={() => router.push("/settings")}
        onRefreshSkills={() => void agent.refreshInventory()}
        onRetryPlugins={() => void agent.refreshPlugins()}
        onInspectSkill={(skill, path) => void agent.inspectSkill(skill, path)}
        onInspectSkillCopy={agent.inspectSkillCopyDetail}
        onDismissInspector={agent.dismissInspector}
        onDeleteSkill={(skill) => void agent.runSkillDelete(skill)}
        onUninstallPlugin={(copy) => void agent.runPluginUninstall(copy)}
        onDismissNotice={agent.dismissNotice}
        onViewSkillPlugin={(pluginKey) => {
          setFocusedPluginKey(pluginKey);
          setChosen("plugins");
        }}
        focusedPluginKey={focusedPluginKey}
        onFocusPluginConsumed={() => setFocusedPluginKey(null)}
      />
    </>
  );
}
