import { describe, expect, open, test } from "nsh/test";

/** The More harness with the current computer's Sessions (`host.params.workers`, `app.workers`). */
const HARNESS = "./testing/Harness.tsx";

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

type Skill = ReturnType<typeof skill>;
type Body = Record<string, unknown>;
type File = { path: string; kind?: string; media_type?: string };

const NOW = "2026-11-11T08:00:00Z";

function detail(copy: Skill, files: File[], path: string | undefined, content: string, extra: Record<string, unknown> = {}) {
  const shown = path ?? (files.some((file) => file.path === "SKILL.md") ? "SKILL.md" : files[0]?.path);
  const file = files.find((entry) => entry.path === shown);
  return {
    copy_id: copy.id, skill_name: copy.name, description: copy.description, enabled: true, root_path: copy.root_path, canonical_path: copy.canonical_path,
    allowed_root: copy.allowed_root, location: copy.location, scope: copy.scope, agents: copy.agents, capability: copy.capability,
    files: files.map((entry) => ({ path: entry.path, size: 20, mode: "0644", kind: entry.kind ?? "markdown", media_type: entry.media_type ?? "text/markdown", preview_status: "ready" })),
    preview: shown ? { path: shown, kind: file?.kind ?? "markdown", media_type: "text/plain", status: "ready", size: content.length, bytes_returned: content.length, content } : undefined,
    ...extra,
  };
}

/** A computer with these Skills and Plugins; `inspect` answers each `skills_inspect`. */
function computer(state: { skills: unknown[]; plugins?: unknown[] }, inspect: (body: Body) => unknown = () => null, extra: (type: string, body: Body) => unknown = () => null) {
  const asked: Array<{ type: string; body: Body }> = [];
  return {
    asked,
    count: (type: string) => asked.filter((entry) => entry.type === type).length,
    last: (type: string) => asked.filter((entry) => entry.type === type).at(-1)?.body,
    host: (page: unknown, workers: unknown[] = []) => ({
      host: {
        params: { page, workers },
        handlers: {
          "app.daemon": ({ type, body }: { type: string; body: Body }) => {
            asked.push({ type, body });
            if (type === "skills_inventory") return { reply: "skills_inventory", generation: body.generation, inventory: { generated_at: NOW, cwd: body.cwd, skills: state.skills, agents: [], mutation_operations: ["delete"], warnings: [] } };
            if (type === "plugins_inventory") return { reply: "plugins_inventory", generation: body.generation, inventory: { generated_at: NOW, installed: state.plugins ?? [], available: [], warnings: [] } };
            if (type === "skills_inspect") {
              const answer = inspect(body);
              if (answer && typeof answer === "object" && "error" in answer) return answer;
              return answer ? { reply: "skills_inspect_result", generation: body.generation, detail: answer } : { error: "no detail" };
            }
            return extra(type, body) ?? { error: `unexpected ${type}` };
          },
        },
      },
    }),
  };
}

const codex = { name: "extensions-agent", scope: "codex" };
const session = (cwd: string, updatedAt: number) => ({ key: cwd, id: cwd, serverId: "s1", serverName: "Desk", serverUrl: "", name: "claude", status: "running", cwd, updated_at: updatedAt, summary: "", last_output_lines: [] });

describe("Project Skills", () => {
  test("Skills are read for the folder the newest Session works in, and the page names that project", async () => {
    const project = skill("deploy", { scope: "project", location: "Codex project Skills", root_path: "/repo/alpha/.codex/skills/deploy", canonical_path: "/repo/alpha/.codex/skills/deploy", allowed_root: "/repo/alpha/.codex/skills" });
    const c = computer({ skills: [skill("review"), project] });
    const app = await open(HARNESS, c.host(codex, [session("/repo/beta", 10), session("/repo/alpha/", 20)]));
    await app.waitFor("id:skill-deploy");
    expect(c.last("skills_inventory")!.cwd).toBe("/repo/alpha/");
    await app.waitFor("label:Project alpha");
    await app.waitFor("text:alpha");
    // The Project scope filter finds it.
    await app.press("id:skills-filter");
    await app.press("id:filter-scope-project");
    await app.press("id:filter-done");
    await app.waitUntil(() => !app.shows("id:skill-review"));
    expect(app.shows("id:skill-deploy")).toBe(true);
  });

  test("another project reads its Skills again and closes the inspector of the last one; alternating activity does not", async () => {
    const review = skill("review");
    const c = computer({ skills: [review] }, (body) => detail(review, [{ path: "SKILL.md" }], body.path as string | undefined, "Read the diff."));
    const app = await open(HARNESS, c.host(codex, [session("/repo/alpha", 20), session("/repo/beta", 10)]));
    await app.waitFor("id:skill-review");
    await app.press("id:skill-review");
    await app.waitFor("id:skill-inspector");
    const reads = c.count("skills_inventory");
    // Beta is busier now, but alpha still has a Session: nothing changes.
    await app.host.send("app.workers", [session("/repo/alpha", 20), session("/repo/beta", 30)]);
    await app.wait(50);
    expect(c.count("skills_inventory")).toBe(reads);
    expect(app.shows("id:skill-inspector")).toBe(true);
    // Alpha's Session is gone: beta is the project now.
    await app.host.send("app.workers", [session("/repo/beta", 30)]);
    await app.waitUntil(() => c.count("skills_inventory") > reads);
    expect(c.last("skills_inventory")!.cwd).toBe("/repo/beta");
    await app.waitFor("label:Project beta");
    expect(app.shows("id:skill-inspector")).toBe(false);
  });

  test("without a Session the global Skills are read and no project is named", async () => {
    const c = computer({ skills: [skill("review")] });
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-review");
    expect(c.last("skills_inventory")!.cwd).toBeUndefined();
    expect(app.shows("id:project-scope")).toBe(false);
  });
});

describe("The Skill inspector, by copy", () => {
  const global = skill("review");
  const projectCopy = skill("review", { id: hex("9", 24), scope: "project", location: "Codex project Skills", root_path: "/repo/alpha/.codex/skills/review", canonical_path: "/repo/alpha/.codex/skills/review", allowed_root: "/repo/alpha/.codex/skills" });

  test("Locations lists each copy; a tap opens that copy's details and files", async () => {
    const c = computer({ skills: [global, projectCopy] }, (body) => {
      const copy = body.skill_id === projectCopy.id ? projectCopy : global;
      return detail(copy, [{ path: "SKILL.md" }], body.path as string | undefined, copy === projectCopy ? "The project's own review." : "The global review.");
    });
    const app = await open(HARNESS, c.host(codex, [session("/repo/alpha", 20)]));
    await app.waitFor("id:skill-review");
    await app.press("id:skill-review");
    await app.waitFor("text:Locations (2)");
    await app.waitFor("text:The global review.");
    await app.press(`id:inspect-copy-${projectCopy.id}`);
    await app.waitFor("text:The project's own review.");
    expect(c.last("skills_inspect")).toMatchObject({ skill_id: projectCopy.id, skill_name: "review" });
    // Its delete is the inspected copy's.
    await app.waitFor("text:Permanently delete this copy from Codex project Skills · Codex.");
  });

  test("a lone copy can be deleted from its inspector, after the computer said what it would do", async () => {
    const lone = skill("lint");
    const c = computer({ skills: [lone] }, (body) => detail(lone, [{ path: "SKILL.md" }], body.path as string | undefined, "Lint."), (type) =>
      type === "skills_command"
        ? { reply: "skills_command", command: { operation: "delete", copy_id: lone.id, skill_name: "lint", root_path: lone.root_path, canonical_path: lone.canonical_path, allowed_root: lone.allowed_root, location: lone.location, scope: "global", agents: ["codex"], summary: "Delete lint", destructive: true } }
        : null,
    );
    const app = await open(HARNESS, c.host(codex));
    app.onDialog(() => "Cancel");
    await app.waitFor("id:skill-lint");
    await app.press("id:skill-lint");
    await app.waitFor("text:Permanently delete this copy from Codex global Skills · Codex.");
    await app.press("label:Delete lint from Codex global Skills · Codex");
    await app.waitUntil(() => c.count("skills_command") === 1);
    expect(c.last("skills_command")).toMatchObject({ operation: "delete", skill_id: lone.id });
  });

  test("a built-in Skill says so, and is protected with no delete", async () => {
    const builtin = skill("system", { scope: "builtin", capability: { can_delete: false } });
    const c = computer({ skills: [builtin] }, (body) => detail(builtin, [{ path: "SKILL.md" }], body.path as string | undefined, "System."));
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-system");
    await app.press("id:skill-system");
    await app.waitFor("text:Built-in");
    await app.waitFor("id:protected");
    expect(app.shows("text:Delete Skill")).toBe(false);
  });

  test("the computer's own refusal in the detail keeps a copy protected", async () => {
    const review = skill("review");
    const c = computer({ skills: [review] }, (body) => detail(review, [{ path: "SKILL.md" }], body.path as string | undefined, "R.", { capability: { can_delete: false, reason: "Managed by policy." } }));
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-review");
    await app.press("id:skill-review");
    await app.waitFor("id:protected");
    expect(app.shows("text:Can delete")).toBe(false);
    expect(app.shows("text:Delete Skill")).toBe(false);
  });

  test("a Skill that could not be read can be tried again", async () => {
    const review = skill("review");
    let fail = true;
    const c = computer({ skills: [review] }, (body) => (fail ? { error: "The Skill folder is unreadable." } : detail(review, [{ path: "SKILL.md" }], body.path as string | undefined, "Now readable.")));
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-review");
    await app.press("id:skill-review");
    await app.waitFor("text:Skill unavailable");
    await app.waitFor("text:The Skill folder is unreadable.");
    fail = false;
    await app.press("id:skill-unavailable-action");
    await app.waitFor("text:Now readable.");
    expect(c.count("skills_inspect")).toBe(2);
  });

  test("an empty copy says it has no readable files", async () => {
    const empty = skill("empty");
    const c = computer({ skills: [empty] }, () => detail(empty, [], undefined, ""));
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-empty");
    await app.press("id:skill-empty");
    await app.waitFor("text:This copy contains no readable files.");
  });
});

describe("A Skill's files", () => {
  const guide = skill("guide");
  const files: File[] = [{ path: "SKILL.md" }, { path: "docs/setup.md" }, { path: "docs/more/notes.md" }];
  const content = (path?: string) => (path === "docs/setup.md" ? "Setup steps here." : path === "docs/more/notes.md" ? "Notes." : "Read [the setup](docs/setup.md) first.");

  test("folders open and close, and the one holding the shown file starts open", async () => {
    const c = computer({ skills: [guide] }, (body) => detail(guide, files, body.path as string | undefined, content(body.path as string | undefined)));
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-guide");
    await app.press("id:skill-guide");
    await app.waitFor("id:skill-folder-docs");
    expect(app.shows("id:skill-file-docs/setup.md")).toBe(false);
    await app.press("id:skill-folder-docs");
    await app.waitFor("id:skill-file-docs/setup.md");
    await app.press("id:skill-file-docs/setup.md");
    await app.waitFor("text:Setup steps here.");
    // Picking a file keeps the folders as they were.
    expect(app.shows("id:skill-folder-docs/more")).toBe(true);
    await app.press("id:skill-folder-docs");
    await app.waitUntil(() => !app.shows("id:skill-file-docs/setup.md"));
  });

  test("a link in SKILL.md to another file of the copy opens that file in place", async () => {
    const c = computer({ skills: [guide] }, (body) => detail(guide, files, body.path as string | undefined, content(body.path as string | undefined)));
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-guide");
    await app.press("id:skill-guide");
    await app.waitFor("text:the setup");
    await app.press("text:the setup");
    await app.waitFor("text:Setup steps here.");
    expect(c.last("skills_inspect")).toMatchObject({ skill_id: guide.id, path: "docs/setup.md" });
  });
});

describe("Copy pickers", () => {
  test("Delete copy names each copy's place and agents, says why one stays, and closes", async () => {
    const global = skill("review");
    const locked = skill("review", { id: hex("8", 24), location: "Codex admin Skills", root_path: "/etc/codex/skills/review", canonical_path: "/etc/codex/skills/review", allowed_root: "/etc/codex/skills", capability: { can_delete: false, reason: "Installed by your administrator." } });
    const c = computer({ skills: [global, locked] });
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:skill-review-action");
    await app.press("id:skill-review-action");
    await app.waitFor("id:skill-copy-picker");
    await app.waitFor("text:Codex global Skills · Codex");
    await app.waitFor("text:Installed by your administrator.");
    await app.press("label:Close copy selector");
    await app.waitUntil(() => !app.shows("id:skill-copy-picker"));
  });

  test("Plugin details follow the copy chosen among several", async () => {
    const first = plugin("helper", { components: [{ kind: "command", name: "first-command" }] });
    const second = plugin("helper", { copy_id: hex("7", 24), version: "2.0.0", location: "Codex project plugins", root_path: "/repo/.codex/plugins/helper", canonical_path: "/repo/.codex/plugins/helper", components: [{ kind: "hook", name: "second-hook" }], capability: { can_uninstall: false, reason: "Pinned by the project." } });
    const c = computer({ skills: [], plugins: [first, second] });
    const app = await open(HARNESS, c.host(codex));
    await app.waitFor("id:plugin-helper");
    await app.press("id:plugin-helper");
    await app.waitFor("text:Copies (2)");
    await app.waitFor("text:first-command");
    await app.waitFor("text:Permanently removes only this exact copy from Codex plugins.");
    await app.press(`id:select-copy-${second.copy_id}`);
    await app.waitFor("text:second-hook");
    expect(app.shows("text:first-command")).toBe(false);
    await app.waitFor("id:protected");
    expect(app.shows("text:Uninstall Plugin")).toBe(false);
  });

  test("No plugins installed offers Refresh", async () => {
    const c = computer({ skills: [skill("review")], plugins: [] });
    const app = await open(HARNESS, c.host({ name: "extensions-agent", scope: "claude-code" }));
    await app.waitFor("id:agent-section-plugins");
    await app.press("id:agent-section-plugins");
    await app.waitFor("text:No plugins installed");
    const reads = c.count("plugins_inventory");
    await app.press("text:Refresh");
    await app.waitUntil(() => c.count("plugins_inventory") > reads);
  });
});
