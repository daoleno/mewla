import type {
  InstalledSkill,
  SkillMutationOperation,
} from "./skillsManagement";

export type SkillsSurfaceSection = "plugins" | "skills";

export type SkillsSurfaceAction = {
  type: "select_section";
  section: SkillsSurfaceSection;
};

export interface SkillsSurfaceState {
  section: SkillsSurfaceSection;
}

export function createSkillsSurfaceState(): SkillsSurfaceState {
  return { section: "skills" };
}

export function reduceSkillsSurface(
  current: SkillsSurfaceState,
  action: SkillsSurfaceAction,
): SkillsSurfaceState {
  if (action.section === current.section) return current;
  return { section: action.section };
}

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
