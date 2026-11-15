import type {
  InstalledSkill,
  SkillMutationOperation,
} from "./skillsManagement";

export type SkillsSurfaceSection = "plugins" | "skills";

/**
 * A Plugin-provided copy is never deleted through the standalone Skills path,
 * even if a daemon reported it as deletable; its Plugin owns removal.
 */
export function isPluginProvidedSkill(skill: InstalledSkill): boolean {
  return skill.scope === "plugin" || Boolean(skill.plugin?.trim());
}

export function skillRowSupportsDelete(
  skill: InstalledSkill,
  capabilities: readonly SkillMutationOperation[],
): boolean {
  return (
    capabilities.includes("delete") &&
    skill.capability.canDelete &&
    !isPluginProvidedSkill(skill)
  );
}
