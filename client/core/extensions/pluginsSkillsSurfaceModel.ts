import type { InstalledSkill, ManagedSkillAgent } from "./skillsManagement";
import { scopeLabel, skillAgentLabel } from "./skillsManagement";

export interface SkillAvailabilityPresentation {
  deletable: boolean;
  summary: string;
  detail: string;
}

export interface LifecycleBadge {
  label: string;
  tone: "neutral" | "accent" | "warning";
}

export function filterInstalledSkills(
  skills: InstalledSkill[],
  query: string,
): InstalledSkill[] {
  return filterByQuery(skills, query, (skill) => [
    skill.name,
    skill.description,
    skill.location,
    skill.rootPath,
    ...skill.agents.map(skillAgentLabel),
    scopeLabel(skill.scope),
  ]);
}

export function installedSkillMetadata(skill: InstalledSkill): string {
  const availableTo = skill.agents.map(skillAgentLabel).join(", ");
  return [skill.location, scopeLabel(skill.scope), availableTo]
    .filter(Boolean)
    .join(" · ");
}

export function installedSkillBadges(
  skill: InstalledSkill,
  installedCount: number,
): LifecycleBadge[] {
  const badges: LifecycleBadge[] = [
    {
      label: skill.enabled ? "Available" : "Unavailable",
      tone: skill.enabled ? "accent" : "warning",
    },
    { label: scopeLabel(skill.scope), tone: "neutral" },
  ];
  if (installedCount > 1) {
    badges.push({ label: `${installedCount} copies`, tone: "neutral" });
  }
  return badges;
}

export function installedSkillAvailability(
  skill: InstalledSkill,
): SkillAvailabilityPresentation {
  const agents = skill.agents.map(skillAgentLabel).join(", ");
  return {
    deletable: skill.capability.canDelete,
    summary: agents ? `Available to ${agents}` : "Local copy",
    detail:
      skill.capability.reason ||
      (skill.capability.canDelete
        ? `Delete removes only the copy at ${skill.location}.`
        : "This copy cannot be deleted from here."),
  };
}

function filterByQuery<T>(
  values: T[],
  query: string,
  searchableValues: (value: T) => Array<string | undefined>,
): T[] {
  const normalized = normalizeSearchText(query);
  if (!normalized) {
    return values;
  }
  return values.filter((value) =>
    searchableValues(value).some((candidate) =>
      normalizeSearchText(candidate || "").includes(normalized),
    ),
  );
}

function normalizeSearchText(value: string): string {
  return value.trim().toLocaleLowerCase();
}
