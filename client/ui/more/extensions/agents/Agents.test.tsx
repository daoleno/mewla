import { describe, expect, open, test } from "nsh/test";

const HARNESS = "../../testing/Harness.tsx";

const hex = (seed: string, length: number) => seed.repeat(length).slice(0, length);

function skill(name: string, overrides: Record<string, unknown> = {}) {
  const root = `/home/me/.codex/skills/${name}`;
  return {
    id: hex(name.charCodeAt(0).toString(16).padStart(2, "0") + "a", 24),
    name,
    description: `${name} description`,
    enabled: true,
    root_path: root,
    canonical_path: root,
    allowed_root: "/home/me/.codex/skills",
    location: "Codex global Skills",
    scope: "global",
    agents: ["codex"],
    capability: { can_delete: true },
    ...overrides,
  };
}

function plugin(name: string, overrides: Record<string, unknown> = {}) {
  const root = `/home/me/.codex/plugins/${name}`;
  return {
    copy_id: hex(name.charCodeAt(0).toString(16).padStart(2, "0") + "b", 24),
    plugin_id: `${name}@market`,
    name,
    display_name: `${name[0]!.toUpperCase()}${name.slice(1)}`,
    description: `${name} plugin`,
    marketplace: "market",
    version: "1.0.0",
    scope: "user",
    enabled: true,
    host: "codex",
    source: "manager",
    root_path: root,
    canonical_path: root,
    allowed_root: "/home/me/.codex/plugins",
    location: "Codex plugins",
    revision: hex("c", 64),
    agents: ["codex"],
    components: [],
    capability: { can_uninstall: true },
    ...overrides,
  };
}

const NOW = "2026-11-11T08:00:00Z";
const skillsInventory = (skills: unknown[]) => ({ generated_at: NOW, skills, agents: [], mutation_operations: ["delete"], warnings: [] });
const pluginsInventory = (installed: unknown[]) => ({ generated_at: NOW, installed, available: [], warnings: [] });

type Body = Record<string, unknown>;

/** A computer whose agents have these Skills and Plugins; `extra` answers anything else. */
function computer(state: { skills: unknown[]; plugins: unknown[] }, extra: (type: string, body: Body) => unknown = () => null) {
  const asked: Array<{ type: string; body: Body }> = [];
  return {
    asked,
    count: (type: string) => asked.filter((entry) => entry.type === type).length,
    host: (page: unknown) => ({
      host: {
        params: { page },
        handlers: {
          "app.daemon": ({ type, body }: { type: string; body: Body }) => {
            asked.push({ type, body });
            if (type === "skills_inventory") return { reply: "skills_inventory", generation: body.generation, inventory: skillsInventory(state.skills) };
            if (type === "plugins_inventory") return { reply: "plugins_inventory", generation: body.generation, inventory: pluginsInventory(state.plugins) };
            return extra(type, body) ?? { error: `unexpected ${type}` };
          },
        },
      },
    }),
  };
}

const page = { name: "extensions-agent", scope: "codex" };

function deleteCommand(copy: ReturnType<typeof skill>) {
  return {
    operation: "delete",
    copy_id: copy.id,
    skill_name: copy.name,
    root_path: copy.root_path,
    canonical_path: copy.canonical_path,
    allowed_root: copy.allowed_root,
    location: copy.location,
    scope: copy.scope,
    agents: copy.agents,
    summary: `Delete ${copy.name}`,
    destructive: true,
  };
}

function uninstallCommand(copy: ReturnType<typeof plugin>) {
  return {
    operation: "uninstall",
    plugin_id: copy.plugin_id,
    host: copy.host,
    scope: "user",
    source: copy.source,
    name: copy.name,
    display_name: copy.display_name,
    agents: copy.agents,
    summary: `Uninstall ${copy.name}`,
    destructive: true,
    copy_id: copy.copy_id,
    version: copy.version,
    root_path: copy.root_path,
    canonical_path: copy.canonical_path,
    allowed_root: copy.allowed_root,
    location: copy.location,
    revision: copy.revision,
  };
}

describe("An agent's own Skills", () => {
  test("a tool with plugins opens on them, and the Skills a plugin brings are listed only inside it", async () => {
    const helper = plugin("helper", { components: [{ kind: "skill", name: "helper-skill", path: "skills/helper-skill" }] });
    const owned = skill("helper-skill", { scope: "plugin", plugin: "helper", root_path: `${helper.root_path}/skills/helper-skill`, canonical_path: `${helper.root_path}/skills/helper-skill`, capability: { can_delete: false } });
    const c = computer({ skills: [skill("review"), owned], plugins: [helper] });
    const app = await open(HARNESS, c.host(page));
    await app.waitFor("id:plugin-helper");
    await app.press("id:agent-section-skills");
    await app.waitFor("id:skill-review");
    expect(app.shows("id:skill-helper-skill")).toBe(false);
    await app.waitFor("text:1 more Skill comes with plugins");
    // View goes back to Plugins, where the plugin's details list the Skill it brings.
    await app.press("text:View");
    await app.press("id:plugin-helper");
    await app.waitFor("id:plugin-details");
    await app.waitFor("text:Skills (1)");
  });

  test("deleting a Skill asks the computer what it would do, confirms, and deletes exactly that copy", async () => {
    const review = skill("review");
    const state = { skills: [review, skill("lint")] as unknown[], plugins: [] as unknown[] };
    const c = computer(state, (type, body) => {
      if (type === "skills_command") return { reply: "skills_command", command: deleteCommand(review) };
      if (type === "skills_mutation") {
        state.skills = state.skills.filter((entry) => (entry as { id: string }).id !== body.skill_id);
        return { reply: "skills_mutation_result", command: deleteCommand(review), success: true, exit_code: 0, duration_ms: 12, output: "" };
      }
      return null;
    });
    const app = await open(HARNESS, c.host(page));
    const asked: string[] = [];
    app.onDialog((dialog) => {
      asked.push(`${dialog.title}\n${dialog.message}`);
      return "Delete";
    });
    await app.waitFor("id:skill-review-action");
    await app.press("id:skill-review-action");
    await app.waitFor("text:Deleted review.");
    expect(asked[0]).toBe('Delete review?\nThis permanently deletes "review".\nAvailable to: Codex\nLocation: Codex global Skills\n\nThis action cannot be undone.');
    const mutation = c.asked.find((entry) => entry.type === "skills_mutation")!.body;
    expect(mutation).toEqual({ operation: "delete", skill_id: review.id, skill_name: "review", root_path: review.root_path, canonical_path: review.canonical_path, allowed_root: review.allowed_root });
    await app.waitUntil(() => !app.shows("id:skill-review"));
    expect(app.shows("id:skill-lint")).toBe(true);
  });

  test("Cancel at the question deletes nothing", async () => {
    const review = skill("review");
    const c = computer({ skills: [review], plugins: [] }, (type) => (type === "skills_command" ? { reply: "skills_command", command: deleteCommand(review) } : null));
    const app = await open(HARNESS, c.host(page));
    app.onDialog(() => "Cancel");
    await app.waitFor("id:skill-review-action");
    await app.press("id:skill-review-action");
    await app.waitUntil(() => c.count("skills_command") === 1);
    await app.wait(50);
    expect(c.count("skills_mutation")).toBe(0);
  });

  test("a protected Skill opens why instead of deleting", async () => {
    const builtin = skill("system", { scope: "builtin", capability: { can_delete: false } });
    const c = computer({ skills: [builtin], plugins: [] }, (type, body) => {
      if (type === "skills_inspect") {
        return { reply: "skills_inspect_result", generation: body.generation, detail: { copy_id: builtin.id, skill_name: "system", enabled: true, root_path: builtin.root_path, canonical_path: builtin.canonical_path, allowed_root: builtin.allowed_root, location: builtin.location, scope: "builtin", agents: ["codex"], capability: { can_delete: false }, files: [] } };
      }
      return null;
    });
    const app = await open(HARNESS, c.host(page));
    await app.waitFor("id:skill-system-action");
    await app.press("id:skill-system-action");
    await app.waitFor("id:skill-inspector");
    await app.waitFor("text:This Skill ships with the Agent itself and cannot be deleted here.");
    expect(c.count("skills_command")).toBe(0);
  });

  test("search narrows the list, and a search with no match offers to clear it", async () => {
    const c = computer({ skills: [skill("review"), skill("lint")], plugins: [] });
    const app = await open(HARNESS, c.host(page));
    await app.waitFor("id:skill-lint");
    await app.type("id:skills-search", "rev");
    await app.waitUntil(() => !app.shows("id:skill-lint"));
    expect(app.shows("id:skill-review")).toBe(true);
    await app.clear("id:skills-search");
    await app.type("id:skills-search", "nothing");
    await app.waitFor("text:No matches");
    await app.press("text:Clear filters");
    await app.waitFor("id:skill-lint");
  });
});

describe("The Skill inspector", () => {
  test("opens on SKILL.md, and another file is read from the same copy", async () => {
    const review = skill("review");
    const files = [
      { path: "SKILL.md", size: 20, mode: "0644", kind: "markdown", media_type: "text/markdown", preview_status: "ready" },
      { path: "config.json", size: 11, mode: "0644", kind: "json", media_type: "application/json", preview_status: "ready" },
    ];
    const c = computer({ skills: [review], plugins: [] }, (type, body) => {
      if (type !== "skills_inspect") return null;
      const path = (body.path as string | undefined) ?? "SKILL.md";
      const content = path === "SKILL.md" ? "# Review\nRead the diff." : '{"depth":2}';
      return {
        reply: "skills_inspect_result",
        generation: body.generation,
        detail: {
          copy_id: review.id, skill_name: "review", description: "Reviews diffs", enabled: true, root_path: review.root_path, canonical_path: review.canonical_path,
          allowed_root: review.allowed_root, location: review.location, scope: "global", agents: ["codex"], capability: { can_delete: true }, files,
          preview: { path, kind: path === "SKILL.md" ? "markdown" : "json", media_type: "text/plain", status: "ready", size: content.length, bytes_returned: content.length, content },
        },
      };
    });
    const app = await open(HARNESS, c.host(page));
    await app.waitFor("id:skill-review");
    await app.press("id:skill-review");
    // SKILL.md is rendered, not shown as its source.
    await app.waitFor("text:Read the diff.");
    expect(app.shows("text:# Review\nRead the diff.")).toBe(false);
    await app.press("id:skill-file-config.json");
    await app.waitFor('text:{\n  "depth": 2\n}');
    expect(c.asked.filter((entry) => entry.type === "skills_inspect").at(-1)!.body).toMatchObject({ skill_name: "review", skill_id: review.id, path: "config.json" });
  });
});

describe("An agent's own Plugins", () => {
  test("a protected Plugin says why and never sends an uninstall", async () => {
    const managed = plugin("managed", { capability: { can_uninstall: false, reason: "Codex manages this Plugin." } });
    const c = computer({ skills: [], plugins: [managed] });
    const app = await open(HARNESS, c.host(page));
    await app.waitFor("id:plugin-managed-action");
    await app.press("id:plugin-managed-action");
    await app.waitFor("id:plugin-details");
    await app.waitFor("text:Codex manages this Plugin.");
    expect(c.count("plugin_command")).toBe(0);
    expect(c.count("plugin_mutation")).toBe(0);
  });

  test("uninstalling reads the inventory again, which decides whether that copy is gone", async () => {
    const helper = plugin("helper");
    const state = { skills: [] as unknown[], plugins: [helper] as unknown[] };
    const c = computer(state, (type) => {
      if (type === "plugin_command") return { reply: "plugin_command", command: uninstallCommand(helper) };
      if (type === "plugin_mutation") {
        state.plugins = [];
        return { reply: "plugin_mutation_result", command: uninstallCommand(helper), success: true, exit_code: 0, duration_ms: 30, output: "" };
      }
      return null;
    });
    const app = await open(HARNESS, c.host(page));
    const titles: string[] = [];
    app.onDialog((dialog) => {
      titles.push(dialog.title ?? "");
      return "Uninstall";
    });
    await app.waitFor("id:plugin-helper-action");
    const readsBefore = c.count("plugins_inventory");
    await app.press("id:plugin-helper-action");
    await app.waitFor("text:Uninstalled Helper.");
    expect(titles).toEqual(["Uninstall Helper?"]);
    expect(c.count("plugins_inventory")).toBe(readsBefore + 1);
    const mutation = c.asked.find((entry) => entry.type === "plugin_mutation")!.body;
    expect(mutation.plugin_copy_id).toBe(helper.copy_id);
    expect(mutation.plugin_revision).toBe(helper.revision);
  });

  test("a run that reports success but leaves the copy installed is not called done", async () => {
    const helper = plugin("helper");
    const c = computer({ skills: [], plugins: [helper] }, (type) => {
      if (type === "plugin_command") return { reply: "plugin_command", command: uninstallCommand(helper) };
      if (type === "plugin_mutation") return { reply: "plugin_mutation_result", command: uninstallCommand(helper), success: true, exit_code: 0, duration_ms: 30, output: "" };
      return null;
    });
    const app = await open(HARNESS, c.host(page));
    app.onDialog(() => "Uninstall");
    await app.waitFor("id:plugin-helper-action");
    await app.press("id:plugin-helper-action");
    await app.waitFor("text:The selected Plugin copy is still installed.");
  });
});

describe("From your agents", () => {
  test("lists each tool with what it installed, and says which have nothing", async () => {
    const shared = skill("shared-skill", { agents: ["codex", "pi"], root_path: "/home/me/.agents/skills/shared-skill", canonical_path: "/home/me/.agents/skills/shared-skill", allowed_root: "/home/me/.agents/skills" });
    const c = computer({ skills: [skill("review"), shared], plugins: [plugin("helper")] });
    const app = await open(HARNESS, c.host({ name: "extensions" }));
    await app.waitFor("id:agent-tool-codex");
    await app.waitFor("text:1 plugin · 1 Skill");
    await app.waitFor("text:1 Skill · used by Codex and Pi");
    await app.waitFor("text:Claude Code, Cursor, Grok, OpenCode and Pi have nothing installed.");
  });
});
