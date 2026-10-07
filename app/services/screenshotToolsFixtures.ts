// Fictional fixtures for the dev-only screenshot route: Skills, Agent
// Plugins, Resources and a Work item. Nothing here reaches a server.
import type { ResourceTelemetry } from "./resourceTelemetry";
import type { InstalledSkill, SkillsInventory } from "./skillsManagement";
import type { InstalledPluginCopy, PluginInventory } from "./pluginsManagement";

const GENERATED_AT = "2026-10-07T06:00:00Z";

function skill(
  name: string,
  description: string,
  agents: InstalledSkill["agents"],
  extra: Partial<InstalledSkill> = {},
): InstalledSkill {
  const root = `/home/demo/.agents/skills/${name}`;
  return {
    id: `copy-${name}`,
    name,
    description,
    enabled: true,
    rootPath: root,
    canonicalPath: root,
    allowedRoot: "/home/demo/.agents/skills",
    location: "~/.agents/skills",
    scope: "global",
    agents,
    capability: { canDelete: true },
    ...extra,
  };
}

export const SCREENSHOT_SKILLS_INVENTORY: SkillsInventory = {
  generatedAt: GENERATED_AT,
  skills: [
    skill("mobile-qa", "Drive the app on a phone and file what breaks.", ["codex", "claude-code"]),
    skill("release-review", "Read a release branch and list what still blocks it.", ["claude-code"]),
    skill("changelog", "Write the changelog from merged pull requests.", ["codex"], { enabled: false }),
    skill("design-audit", "Check a screen against the design language.", ["codex", "claude-code", "opencode"]),
    skill("frontend-design", "Build distinctive interfaces.", ["claude-code"], {
      id: "copy-frontend-design",
      scope: "plugin",
      plugin: "frontend-design@claude-plugins",
      capability: { canDelete: false, reason: "Provided by a Plugin" },
    }),
  ],
  agents: [],
  warnings: [],
  mutationOperations: ["delete"],
};

function plugin(name: string, displayName: string, description: string, host: InstalledPluginCopy["host"]): InstalledPluginCopy {
  const root = `/home/demo/.${host}/plugins/${name}`;
  return {
    copyId: `plugin-${name}-${host}`,
    pluginId: `${name}@claude-plugins`,
    name,
    displayName,
    description,
    marketplace: "claude-plugins",
    version: "1.2.0",
    scope: "user",
    enabled: true,
    host,
    source: "manager",
    rootPath: root,
    canonicalPath: root,
    allowedRoot: `/home/demo/.${host}/plugins`,
    location: `~/.${host}/plugins`,
    revision: "r1",
    agents: [host === "claude" ? "claude-code" : "codex"],
    components: [{ kind: "skill", name: "frontend-design" }],
    capability: { canUninstall: true },
  };
}

export const SCREENSHOT_PLUGIN_INVENTORY: PluginInventory = {
  generatedAt: GENERATED_AT,
  installed: [
    plugin("frontend-design", "Frontend Design", "Distinctive, production-grade interfaces.", "claude"),
    plugin("pr-review", "PR Review", "Review pull requests with several focused agents.", "codex"),
  ],
  available: [],
  warnings: [],
};

/** A busy but healthy machine, plus one elevated memory signal. */
export function screenshotResourceTelemetry(now = Date.now()): ResourceTelemetry {
  const GB = 1024 ** 3;
  const history = Array.from({ length: 60 }, (_, index) => {
    const wave = Math.sin(index / 6);
    return {
      sampledAt: now - (59 - index) * 10_000,
      state: index > 50 ? ("elevated" as const) : ("normal" as const),
      memoryAvailableBytes: (9 - wave * 2) * GB,
      memoryUsedBytes: (23 + wave * 2) * GB,
      swapUsedBytes: 0.4 * GB,
      load15: 3.2,
      cpuPercent: 38 + wave * 18,
      psiCpu: 2 + Math.max(0, wave) * 4,
      psiMemory: index > 50 ? 14 : 3 + Math.max(0, wave) * 3,
      psiIo: 1 + Math.max(0, -wave) * 2,
      diskReadBytesPerSecond: (4 + wave * 3) * 1024 ** 2,
      diskWriteBytesPerSecond: (2 + Math.max(0, wave) * 5) * 1024 ** 2,
    };
  });
  return {
    version: 2,
    sampledAt: now,
    state: "elevated",
    cpu: {
      load1: 4.1,
      load5: 3.6,
      load15: 3.2,
      utilizationPercent: 41,
      perCorePercent: [52, 38, 61, 22, 44, 30, 57, 19, 35, 48, 27, 40],
    },
    memory: {
      totalBytes: 32 * GB,
      availableBytes: 7.2 * GB,
      usedBytes: 24.8 * GB,
      cacheBytes: 5.1 * GB,
      sharedBytes: 0.6 * GB,
      swapTotalBytes: 8 * GB,
      swapUsedBytes: 0.4 * GB,
    },
    psi: {
      cpu: { some: { avg10: 3.1, avg60: 2.4, avg300: 1.9 } },
      memory: { some: { avg10: 14.2, avg60: 9.8, avg300: 4.1 }, full: { avg10: 2.1, avg60: 1.2, avg300: 0.4 } },
      io: { some: { avg10: 1.4, avg60: 1.1, avg300: 0.9 } },
    },
    disks: [
      { mount: "/", totalBytes: 512 * GB, usedBytes: 301 * GB, freeBytes: 211 * GB, readBytesPerSecond: 5.2 * 1024 ** 2, writeBytesPerSecond: 3.1 * 1024 ** 2 },
      { mount: "/data", totalBytes: 2048 * GB, usedBytes: 1880 * GB, freeBytes: 168 * GB },
    ],
    consumers: [
      {
        id: "w-atlas",
        commands: ["codex"],
        processes: [{ pid: 4211, command: "codex", rssBytes: 1.9 * GB }],
        owner: "worker",
        workerId: "w-atlas",
        title: "atlas-notes · release checklist",
        status: "running",
        executor: "codex",
        ageSeconds: 2400,
        rssBytes: 3.4 * GB,
        cpuPercent: 64,
        processCount: 12,
        kinds: ["agent", "node"],
      },
      {
        commands: ["node"],
        processes: [{ pid: 3120, command: "expo start", rssBytes: 2.2 * GB }],
        owner: "orphaned_worker",
        title: "weather-kit · old Expo server",
        status: "exited",
        ageSeconds: 86_000,
        rssBytes: 2.2 * GB,
        cpuPercent: 3,
        processCount: 4,
        kinds: ["node"],
      },
      {
        commands: ["mewla"],
        processes: [{ pid: 901, command: "mewla brain", rssBytes: 0.6 * GB }],
        owner: "brain",
        title: "Brain",
        status: "idle",
        rssBytes: 0.6 * GB,
        cpuPercent: 1,
        processCount: 2,
        kinds: ["brain"],
      },
      {
        commands: ["postgres"],
        processes: [{ pid: 777, command: "postgres", rssBytes: 0.9 * GB }],
        owner: "docker",
        title: "postgres",
        rssBytes: 0.9 * GB,
        cpuPercent: 2,
        processCount: 6,
        kinds: ["container"],
      },
    ],
    history,
    signals: [{ name: "memory_psi_some_avg10", value: 14.2, threshold: 10, state: "elevated" }],
  };
}

export const SCREENSHOT_WORK_SERVER_ID = "demo-server";

/** One Work item per state the Work screen distinguishes. */
export function screenshotWorkItems(now = Date.now()) {
  const minutesAgo = (minutes: number) => new Date(now - minutes * 60_000).toISOString();
  const body = (title: string) =>
    `# ${title}\n\nCheck the release branch on a phone before the customer call.\n\n- [x] Build the Android preview\n- [ ] Run the mobile QA pass\n- [ ] Write the release notes\n`;
  const item = (id: string, title: string, frontmatter: Record<string, unknown>) => ({
    id,
    path: `work/${id}.md`,
    project: "atlas-notes",
    title,
    body: body(title),
    frontmatter: { id, created: minutesAgo(120), title, ...frontmatter },
    mtime: minutesAgo(4),
  });
  return [
    item("w-running", "Ship the release checklist", { started: minutesAgo(30), status: "running" }),
    item("w-blocked", "Wait for the signing key rotation", { status: "blocked" }),
    item("w-failed", "Regenerate the store screenshots", { status: "failed" }),
    item("w-done", "Fix the reminder timezone bug", { done: minutesAgo(10) }),
    item("w-queued", "Draft the onboarding copy", {}),
  ];
}
