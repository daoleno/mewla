import { codedRequest, type Daemon } from "../more/daemon";
import type { ConnectionRequest, ConnectionResponse } from "./connections";
import { PLUGIN_COMMAND_TIMEOUT_MS, PLUGIN_MUTATION_TIMEOUT_MS, PLUGINS_INVENTORY_TIMEOUT_MS, SKILLS_MUTATION_TIMEOUT_MS } from "./pluginsDeadlines";
import {
  assertPluginCommandMatchesRequest,
  assertPluginMutationMatchesRequest,
  normalizePluginMutationCommand,
  normalizePluginMutationResult,
  normalizePluginsInventory,
  type PluginInventory,
  type PluginMutationCommand,
  type PluginMutationInput,
  type PluginMutationResult,
} from "./pluginsManagement";
import {
  assertSkillsCommandMatchesRequest,
  assertSkillsMutationMatchesRequest,
  normalizeSkillsInspectDetail,
  normalizeSkillsInventory,
  normalizeSkillsMutationCommand,
  normalizeSkillsMutationResult,
  type PackageDetail,
  type SkillDeleteIdentity,
  type SkillsInventory,
  type SkillsMutationCommand,
  type SkillsMutationResult,
} from "./skillsManagement";

/** What Extensions asks the current computer, over its socket. */

const DISCONNECTED = "Server disconnected. Reconnect to check the result.";

export async function requestConnections(daemon: Daemon, request: ConnectionRequest): Promise<ConnectionResponse> {
  const frame = await codedRequest<{ connections?: unknown }>(daemon, "connections", { connection_request: request }, {
    reply: "connections_result",
    timeoutMs: 25_000,
    timeout: "Plugin request timed out. Refresh to check the result.",
    failure: "Plugin request failed.",
    disconnected: DISCONNECTED,
  });
  if (!frame.connections || typeof frame.connections !== "object") throw new Error("Invalid plugin response.");
  return frame.connections as ConnectionResponse;
}

export async function getSkillsInventory(daemon: Daemon, options: { generation: number; cwd?: string }): Promise<SkillsInventory> {
  const frame = await codedRequest<{ generation?: number; inventory?: unknown }>(daemon, "skills_inventory", { generation: options.generation, cwd: options.cwd || undefined }, {
    reply: "skills_inventory",
    errors: ["skills_inventory_error"],
    timeoutMs: 15_000,
    timeout: "Timed out while loading installed Skills.",
    failure: "Failed to load installed Skills.",
    disconnected: DISCONNECTED,
  });
  if (frame.generation !== options.generation) throw new Error("Daemon returned a stale Skills inventory generation.");
  return normalizeSkillsInventory(frame.inventory);
}

export async function inspectSkill(
  daemon: Daemon,
  options: { generation: number; skillName: string; skillId?: string; cwd?: string; path?: string },
): Promise<PackageDetail> {
  const frame = await codedRequest<{ generation?: number; detail?: unknown }>(
    daemon,
    "skills_inspect",
    { generation: options.generation, skill_name: options.skillName, skill_id: options.skillId, cwd: options.cwd || undefined, path: options.path },
    { reply: "skills_inspect_result", errors: ["skills_inspect_error"], timeoutMs: 15_000, timeout: "Timed out while inspecting the Skill.", failure: "Could not inspect this Skill.", disconnected: DISCONNECTED },
  );
  if (frame.generation !== options.generation) throw new Error("Daemon returned a stale Skills inspect generation.");
  const detail = normalizeSkillsInspectDetail(frame.detail);
  if (detail.skillName !== options.skillName || (options.skillId != null && detail.copyId !== options.skillId)) {
    throw new Error("Daemon returned details for a different Skill copy.");
  }
  return detail;
}

function skillIdentityFields(options: SkillDeleteIdentity): Record<string, unknown> {
  return {
    operation: options.operation,
    cwd: options.cwd,
    skill_id: options.skillId,
    skill_name: options.skillName,
    root_path: options.rootPath,
    canonical_path: options.canonicalPath,
    allowed_root: options.allowedRoot,
  };
}

/** The computer's own account of what a delete would do, shown before asking. */
export async function previewSkillDelete(daemon: Daemon, identity: SkillDeleteIdentity): Promise<SkillsMutationCommand> {
  const frame = await codedRequest<{ command?: unknown }>(daemon, "skills_command", skillIdentityFields(identity), {
    reply: "skills_command",
    errors: ["skills_command_error"],
    timeoutMs: 15_000,
    timeout: "Timed out while validating the Skills command.",
    failure: "Skills command was rejected.",
    disconnected: DISCONNECTED,
  });
  const command = normalizeSkillsMutationCommand(frame.command);
  assertSkillsCommandMatchesRequest(command, identity);
  return command;
}

export async function deleteSkill(daemon: Daemon, identity: SkillDeleteIdentity): Promise<SkillsMutationResult> {
  const frame = await codedRequest<Record<string, unknown>>(daemon, "skills_mutation", skillIdentityFields(identity), {
    reply: "skills_mutation_result",
    errors: ["skills_mutation_error"],
    timeoutMs: SKILLS_MUTATION_TIMEOUT_MS,
    timeout: "Timed out while running the Skills mutation.",
    failure: "The Skills mutation failed.",
    disconnected: DISCONNECTED,
  });
  const result = normalizeSkillsMutationResult(frame);
  assertSkillsMutationMatchesRequest(result, identity);
  return result;
}

export async function getPluginsInventory(daemon: Daemon, options: { generation: number }): Promise<PluginInventory> {
  const frame = await codedRequest<{ generation?: number; inventory?: unknown }>(daemon, "plugins_inventory", { generation: options.generation }, {
    reply: "plugins_inventory",
    errors: ["plugins_inventory_error"],
    timeoutMs: PLUGINS_INVENTORY_TIMEOUT_MS,
    timeout: "Timed out while loading Plugins.",
    failure: "Failed to load Plugins.",
    disconnected: DISCONNECTED,
  });
  if (frame.generation !== options.generation) throw new Error("Daemon returned a stale Plugins inventory generation.");
  return normalizePluginsInventory(frame.inventory);
}

function pluginMutationFields(options: PluginMutationInput): Record<string, unknown> {
  const uninstall = options.operation === "uninstall";
  return {
    operation: options.operation,
    plugin_id: options.pluginId,
    plugin_host: options.host,
    plugin_source: uninstall ? options.source : undefined,
    plugin_version: uninstall ? options.version : undefined,
    agents: uninstall ? options.agents : undefined,
    scope: options.scope,
    plugin_copy_id: uninstall ? options.copyId : undefined,
    plugin_name: uninstall ? options.name : undefined,
    root_path: uninstall ? options.rootPath : undefined,
    canonical_path: uninstall ? options.canonicalPath : undefined,
    allowed_root: uninstall ? options.allowedRoot : undefined,
    plugin_revision: uninstall ? options.revision : undefined,
  };
}

export async function previewPluginMutation(daemon: Daemon, input: PluginMutationInput): Promise<PluginMutationCommand> {
  const frame = await codedRequest<{ command?: unknown }>(daemon, "plugin_command", pluginMutationFields(input), {
    reply: "plugin_command",
    errors: ["plugin_command_error"],
    timeoutMs: PLUGIN_COMMAND_TIMEOUT_MS,
    timeout: "Timed out while validating the plugin command.",
    failure: "Plugin command was rejected.",
    disconnected: DISCONNECTED,
  });
  const command = normalizePluginMutationCommand(frame.command);
  assertPluginCommandMatchesRequest(command, input);
  return command;
}

export async function runPluginMutation(daemon: Daemon, input: PluginMutationInput): Promise<PluginMutationResult> {
  const frame = await codedRequest<Record<string, unknown>>(daemon, "plugin_mutation", pluginMutationFields(input), {
    reply: "plugin_mutation_result",
    errors: ["plugin_mutation_error"],
    timeoutMs: PLUGIN_MUTATION_TIMEOUT_MS,
    timeout: "Timed out while running the Plugin mutation.",
    failure: "The Plugin mutation failed.",
    disconnected: DISCONNECTED,
  });
  const result = normalizePluginMutationResult(frame);
  assertPluginMutationMatchesRequest(result, input);
  return result;
}
