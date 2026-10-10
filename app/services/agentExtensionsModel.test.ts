import { describe, expect, test } from "bun:test";
import type { InstalledSkill } from "./skillsManagement";
import type { InstalledPluginCopy } from "./pluginsManagement";
import {
  agentExtensionsCountLabel,
  agentExtensionsSummaries,
  isAgentExtensionsScope,
  joinAgentLabels,
  pluginsForScope,
  skillCopyScope,
  skillsForScope,
} from "./agentExtensionsModel";

let sequence = 0;
const skill = (name: string, agents: InstalledSkill["agents"], overrides: Partial<InstalledSkill> = {}): InstalledSkill => ({
  id: String(++sequence).padStart(24, "a"),
  name,
  enabled: true,
  rootPath: `/home/test/${agents.join("-") || "store"}/${name}`,
  canonicalPath: `/home/test/${agents.join("-") || "store"}/${name}`,
  allowedRoot: "/home/test",
  location: "Skills",
  scope: "global",
  agents,
  capability: { canDelete: true },
  ...overrides,
});

const plugin = (name: string, host: InstalledPluginCopy["host"]): InstalledPluginCopy => ({
  copyId: String(++sequence).padStart(24, "c"),
  pluginId: `${name}@market`,
  name,
  marketplace: "market",
  scope: "user",
  enabled: true,
  host,
  source: "manager",
  rootPath: `/home/test/${host}/${name}`,
  canonicalPath: `/home/test/${host}/${name}`,
  allowedRoot: "/home/test",
  location: "plugins",
  revision: "r".repeat(64),
  agents: [host === "claude" ? "claude-code" : "codex"],
  components: [],
  capability: { canUninstall: true },
});

describe("agent extensions scopes", () => {
  test("a copy for one tool is that tool's; several tools or none is Shared", () => {
    expect(skillCopyScope(skill("a", ["codex"]))).toBe("codex");
    expect(skillCopyScope(skill("b", ["codex", "pi"]))).toBe("shared");
    expect(skillCopyScope(skill("c", []))).toBe("shared");
  });

  test("each copy is listed under exactly one scope", () => {
    const copies = [
      skill("review", ["codex"]),
      skill("review", ["claude-code"]),
      skill("notes", ["codex", "pi"]),
    ];
    expect(skillsForScope(copies, "codex").map((row) => [row.name, row.copies.length])).toEqual([["review", 1]]);
    expect(skillsForScope(copies, "claude-code").map((row) => row.name)).toEqual(["review"]);
    expect(skillsForScope(copies, "pi")).toEqual([]);
    expect(skillsForScope(copies, "shared").map((row) => row.name)).toEqual(["notes"]);
  });

  test("plugins belong to their host and never to Shared", () => {
    const copies = [plugin("github", "codex"), plugin("github", "claude"), plugin("vercel", "codex")];
    expect(pluginsForScope(copies, "codex").map((row) => row.name)).toEqual(["github", "vercel"]);
    expect(pluginsForScope(copies, "claude-code").map((row) => row.copies.length)).toEqual([1]);
    expect(pluginsForScope(copies, "shared")).toEqual([]);
  });

  test("route params name a known scope", () => {
    expect(isAgentExtensionsScope("claude-code")).toBe(true);
    expect(isAgentExtensionsScope("shared")).toBe(true);
    expect(isAgentExtensionsScope("github")).toBe(false);
    expect(isAgentExtensionsScope(undefined)).toBe(false);
  });
});

describe("agent extensions summaries", () => {
  test("rows follow tool order, Shared last, and tools with nothing installed fold away", () => {
    const { rows, empty } = agentExtensionsSummaries(
      [skill("a", ["pi"]), skill("b", ["codex"]), skill("c", ["codex", "pi"]), skill("d", ["codex", "pi"])],
      [plugin("github", "codex"), plugin("figma", "claude")],
    );
    expect(rows.map((row) => [row.scope, row.plugins, row.skills])).toEqual([
      ["codex", 1, 1],
      ["claude-code", 1, 0],
      ["pi", 0, 1],
      ["shared", 0, 2],
    ]);
    expect(rows.at(-1)!.agents).toEqual(["codex", "pi"]);
    expect(empty).toEqual(["cursor", "grok", "opencode"]);
  });

  test("labels read as counts, and tool lists read as prose", () => {
    expect(agentExtensionsCountLabel({ plugins: 1, skills: 0 })).toBe("1 plugin");
    expect(agentExtensionsCountLabel({ plugins: 6, skills: 34 })).toBe("6 plugins · 34 Skills");
    expect(agentExtensionsCountLabel({ plugins: 0, skills: 1 })).toBe("1 Skill");
    expect(joinAgentLabels(["codex"])).toBe("Codex");
    expect(joinAgentLabels(["codex", "pi"])).toBe("Codex and Pi");
    expect(joinAgentLabels(["codex", "claude-code", "pi"])).toBe("Codex, Claude Code and Pi");
  });
});
