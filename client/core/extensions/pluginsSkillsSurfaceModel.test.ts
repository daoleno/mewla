import { describe, expect, test } from "bun:test";
import {
  filterInstalledSkills,
  installedSkillAvailability,
  installedSkillMetadata,
} from "./pluginsSkillsSurfaceModel";
import type { InstalledSkill } from "./skillsManagement";

function skill(
  name: string,
  overrides: Partial<InstalledSkill> = {},
): InstalledSkill {
  return {
    id: name.padEnd(24, "0").slice(0, 24),
    name,
    description: `${name} description`,
    enabled: true,
    rootPath: `/home/test/.codex/skills/${name}`,
    canonicalPath: `/home/test/.codex/skills/${name}`,
    allowedRoot: "/home/test/.codex/skills",
    location: "Codex global Skills",
    scope: "global",
    agents: ["codex"],
    capability: { canDelete: true },
    ...overrides,
  };
}

describe("shared Plugins and Skills geometry", () => {
  test("local Skill search and availability remain daemon-derived", () => {
    const local = skill("alpha", { description: "Native provider helper" });
    expect(filterInstalledSkills([local], "native provider")).toEqual([local]);
    expect(installedSkillMetadata(local)).toBe(
      "Codex global Skills · Global · Codex",
    );
    expect(installedSkillAvailability(local)).toEqual({
      deletable: true,
      summary: "Available to Codex",
      detail: "Delete removes only the copy at Codex global Skills.",
    });
  });
});
