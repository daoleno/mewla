import { effect, signal, untracked } from "nsh";
import { AlertDialog } from "nsh/base";
import { failureText, type Daemon } from "../../../../core/more/daemon";
import { groupLogicalPlugins } from "../../../../core/extensions/pluginsScreenModel";
import { evaluatePluginUninstall, reconcilePluginUninstallInventory } from "../../../../core/extensions/pluginsScreenModel";
import { buildPluginMutationConfirmation, pluginUninstallInput, type InstalledPluginCopy, type PluginInventory } from "../../../../core/extensions/pluginsManagement";
import { deleteSkill, getPluginsInventory, getSkillsInventory, inspectSkill as inspectSkillRequest, previewPluginMutation, previewSkillDelete, runPluginMutation } from "../../../../core/extensions/requests";
import { skillsOutsidePlugins } from "../../../../core/extensions/skillsPluginOwnership";
import { isPluginProvidedSkill, skillRowSupportsDelete } from "../../../../core/extensions/skillsSurfaceModel";
import {
  beginSkillsRequest,
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
  skillsRequestData,
  type InstalledSkill,
  type PackageDetail,
  type SkillsInventory,
  type SkillsRequestState,
} from "../../../../core/extensions/skillsManagement";
import { selectStableSkillsProjectCwd } from "../../../../core/extensions/skillsScreenModel";
import { workers } from "../../../session";
import { daemon } from "../../daemon";

/**
 * What the current computer's agents installed themselves: the Skills and
 * Plugins inventories, the Skill being inspected, the one delete or uninstall
 * in progress and how it ended. One computer at a time: another computer drops
 * all of it, and a late answer for the last one changes nothing.
 */

export type MutationNotice = { kind: "success" | "error"; message: string };

export const skillsState = signal<SkillsRequestState<SkillsInventory>>(createSkillsRequestState());
export const pluginsState = signal<SkillsRequestState<PluginInventory>>(createSkillsRequestState());
/** The copy the inspector shows, and its detail. */
export const inspected = signal<InstalledSkill | null>(null);
export const inspectState = signal<SkillsRequestState<PackageDetail>>(createSkillsRequestState());
/** `delete:<copy id>` or `uninstall:<copy id>` while one is being prepared or run. */
export const preparing = signal("");
export const notice = signal<MutationNotice | null>(null);
/**
 * The project the Skills are read for: the folder of the current computer's
 * newest Session, kept while a Session still works there (so agents taking
 * turns in two folders don't flip the list). "" with no Session: global Skills.
 */
export const projectCwd = signal("");

let serverId = "";
let projectServer: string | null = null;
let skillsGeneration = 0;
let pluginsGeneration = 0;
let inspectGeneration = 0;
/** Reads of a Skill's files outside the inspector (a Plugin's Skills, pictures): never the inspector's own. */
let sideReads = 0;
let mutationOwner = 0;

// Another project: its Skills are read again and the inspector of the last one closes.
effect(() => {
  const list = workers();
  const id = daemon()?.server.id ?? null;
  untracked(() => {
    const previous = projectServer === id ? projectCwd() : "";
    projectServer = id;
    const next = selectStableSkillsProjectCwd(list, id, previous);
    if (next === projectCwd()) return;
    projectCwd.set(next);
    const wasRead = skillsState().status !== "idle";
    skillsGeneration++;
    inspectGeneration++;
    skillsState.set(createSkillsRequestState());
    inspected.set(null);
    inspectState.set(createSkillsRequestState());
    notice.set(null);
    // Before `followComputer` has taken a new computer, its own first read uses this project.
    if (wasRead && id && id === serverId) void refreshSkills();
  });
});

function computer(): Daemon | null {
  const current = untracked(daemon);
  return current && current.server.id === serverId ? current : null;
}

/** Another computer: nothing of the last one stays, and its late answers are dropped. */
export function followComputer(id: string): void {
  if (id === serverId) return;
  serverId = id;
  skillsGeneration++;
  pluginsGeneration++;
  inspectGeneration++;
  mutationOwner++;
  skillsState.set(createSkillsRequestState());
  pluginsState.set(createSkillsRequestState());
  inspected.set(null);
  inspectState.set(createSkillsRequestState());
  preparing.set("");
  notice.set(null);
}

export async function refreshSkills(): Promise<void> {
  const generation = ++skillsGeneration;
  skillsState.set(beginSkillsRequest(untracked(skillsState), generation));
  const current = computer();
  if (!current) {
    skillsState.set(failSkillsRequest(untracked(skillsState), generation, "Choose a current server in Settings to view local Skills.", false));
    return;
  }
  if (current.state !== "connected") {
    skillsState.set(failSkillsRequest(untracked(skillsState), generation, "The current server is offline. Connect it in Settings and retry.", false));
    return;
  }
  try {
    const inventory = await getSkillsInventory(current, { generation, cwd: untracked(projectCwd) });
    if (generation !== skillsGeneration) return;
    skillsState.set(completeSkillsRequest(untracked(skillsState), generation, inventory, inventory.skills.length === 0));
  } catch (error) {
    if (generation === skillsGeneration) skillsState.set(failSkillsRequest(untracked(skillsState), generation, failureText(error, "Failed to read local Skills.")));
  }
}

export async function refreshPlugins(): Promise<PluginInventory | undefined> {
  const generation = ++pluginsGeneration;
  pluginsState.set(beginSkillsRequest(untracked(pluginsState), generation));
  const current = computer();
  if (!current || current.state !== "connected") {
    pluginsState.set(failSkillsRequest(untracked(pluginsState), generation, "Connect the current server to manage Plugins.", false));
    return undefined;
  }
  try {
    const inventory = await getPluginsInventory(current, { generation });
    if (generation !== pluginsGeneration) return undefined;
    pluginsState.set(completeSkillsRequest(untracked(pluginsState), generation, inventory, inventory.installed.length === 0));
    return inventory;
  } catch (error) {
    if (generation === pluginsGeneration) pluginsState.set(failSkillsRequest(untracked(pluginsState), generation, failureText(error, "Failed to load Plugins.")));
    return undefined;
  }
}

/** Both inventories; Plugin ownership has to be known before Skills are split by tool. */
export function refreshAll(): void {
  void refreshPlugins();
  void refreshSkills();
}

/** Every Skill copy, Plugin-owned ones included. */
export function allSkills(): InstalledSkill[] {
  return skillsRequestData(skillsState())?.skills ?? [];
}

export function pluginCopies(): InstalledPluginCopy[] {
  return skillsRequestData(pluginsState())?.installed ?? [];
}

export function mutationOperations() {
  return skillsRequestData(skillsState())?.mutationOperations ?? [];
}

/**
 * The Skill copies no installed Plugin owns: the ones listed by tool. Until the
 * Plugins inventory has been read, every copy is listed (and protected), so
 * ownership is never guessed from half an inventory.
 */
export function listableSkills(): InstalledSkill[] {
  const plugins = pluginsState();
  const resolved = plugins.status === "ready" || plugins.status === "empty";
  return resolved ? skillsOutsidePlugins(allSkills(), groupLogicalPlugins(pluginCopies())) : allSkills();
}

/** Opens the inspector on one copy (or one of its files). */
export async function inspectSkill(copy: InstalledSkill, path?: string): Promise<void> {
  const generation = ++inspectGeneration;
  inspected.set(copy);
  inspectState.set(beginSkillsRequest(untracked(inspectState), generation, !!path));
  const current = computer();
  if (!current || current.state !== "connected") {
    inspectState.set(failSkillsRequest(untracked(inspectState), generation, "Connect the current server to inspect this Skill.", false));
    return;
  }
  try {
    const detail = await inspectSkillRequest(current, { generation, skillName: copy.name, skillId: copy.id, cwd: untracked(projectCwd), path });
    if (generation !== inspectGeneration) return;
    if (detail.copyId !== copy.id || detail.skillName !== copy.name) throw new Error("The selected Skill copy changed while it was loading.");
    inspectState.set(completeSkillsRequest(untracked(inspectState), generation, detail, false));
  } catch (error) {
    if (generation === inspectGeneration) inspectState.set(failSkillsRequest(untracked(inspectState), generation, failureText(error, "Could not inspect this Skill.")));
  }
}

/** A file of a copy shown inside a Plugin's details; never touches the Skills inspector. */
export async function readSkillCopy(copy: InstalledSkill, path?: string): Promise<PackageDetail> {
  const current = computer();
  if (!current || current.state !== "connected") throw new Error("Connect the current server to inspect this Skill.");
  const detail = await inspectSkillRequest(current, { generation: ++sideReads, skillName: copy.name, skillId: copy.id, cwd: untracked(projectCwd), path });
  if (detail.copyId !== copy.id || detail.skillName !== copy.name) throw new Error("The selected Skill copy changed while it was loading.");
  return detail;
}

export function closeInspector(): void {
  inspectGeneration++;
  inspected.set(null);
  inspectState.set(createSkillsRequestState());
}

/** How long a success stays before it leaves by itself; a failure stays until dismissed. */
export const SUCCESS_NOTICE_MS = 4_000;

function showNotice(next: MutationNotice): void {
  notice.set(next);
  if (next.kind === "success") setTimeout(() => untracked(notice) === next && notice.set(null), SUCCESS_NOTICE_MS);
}

async function confirm(title: string, message: string, label: string): Promise<boolean> {
  const answer = await AlertDialog.show({
    title,
    description: message,
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "go", label, role: "destructive" },
    ],
  });
  return answer.action === "go";
}

/**
 * Deletes exactly one Skill copy: the computer says what it would do, the
 * person confirms, then it runs. A copy a Plugin provides, or one the computer
 * protects, is never sent.
 */
export async function deleteSkillCopy(copy: InstalledSkill): Promise<void> {
  const current = computer();
  if (!current || current.state !== "connected" || untracked(preparing)) return;
  if (!skillRowSupportsDelete(copy, untracked(mutationOperations)) || isPluginProvidedSkill(copy)) return;
  const owner = ++mutationOwner;
  const mine = () => owner === mutationOwner;
  notice.set(null);
  preparing.set(`delete:${copy.id}`);
  try {
    const identity = {
      operation: "delete" as const,
      cwd: untracked(projectCwd) || undefined,
      skillId: copy.id,
      skillName: copy.name,
      rootPath: copy.rootPath,
      canonicalPath: copy.canonicalPath,
      allowedRoot: copy.allowedRoot,
    };
    const command = await previewSkillDelete(current, identity);
    if (!mine()) return;
    const ask = buildSkillsMutationConfirmation(command);
    if (!(await confirm(ask.title, ask.message, ask.confirmLabel)) || !mine()) return;
    const result = await deleteSkill(current, identity);
    if (!mine()) return;
    showNotice({
      kind: result.execution.success ? "success" : "error",
      message: result.execution.success ? `Deleted ${copy.name}.` : result.execution.output || "The Skill could not be deleted.",
    });
    if (result.execution.success) {
      await refreshSkills();
      if (mine() && untracked(inspected)?.id === copy.id) closeInspector();
    }
  } catch (error) {
    if (mine()) notice.set({ kind: "error", message: failureText(error, "The Skill could not be deleted.") });
  } finally {
    if (mine()) preparing.set("");
  }
}

/**
 * Uninstalls exactly one Plugin copy. A protected copy says why and sends
 * nothing. Whatever the run answers, the inventory is read again and decides
 * whether that copy is gone.
 */
export async function uninstallPluginCopy(copy: InstalledPluginCopy): Promise<void> {
  const current = computer();
  if (!current || current.state !== "connected" || untracked(preparing)) return;
  const evaluation = evaluatePluginUninstall(copy);
  if (!evaluation.supported) {
    notice.set({ kind: "error", message: evaluation.reason });
    return;
  }
  const owner = ++mutationOwner;
  const mine = () => owner === mutationOwner;
  notice.set(null);
  preparing.set(`uninstall:${copy.copyId}`);
  try {
    const input = pluginUninstallInput(copy);
    const command = await previewPluginMutation(current, input);
    if (!mine()) return;
    const ask = buildPluginMutationConfirmation(command);
    if (!(await confirm(ask.title, ask.message, ask.confirmLabel)) || !mine()) return;
    let mutationError = "";
    try {
      const result = await runPluginMutation(current, input);
      if (!result.execution.success) mutationError = result.execution.output || "The Plugin could not be uninstalled.";
    } catch (error) {
      mutationError = failureText(error, "The Plugin could not be uninstalled.");
    }
    if (!mine()) return;
    const refreshed = await refreshPlugins();
    if (!mine()) return;
    if (!refreshed) {
      notice.set({ kind: "error", message: mutationError || "The Plugin inventory could not confirm the uninstall." });
      return;
    }
    const outcome = reconcilePluginUninstallInventory(copy, refreshed);
    if (!outcome.removed) {
      notice.set({ kind: "error", message: mutationError || "The selected Plugin copy is still installed." });
      return;
    }
    const name = command.displayName || command.name;
    const left = outcome.remainingNamedCopies;
    showNotice({
      kind: "success",
      message: left ? `Uninstalled the ${name} copy from ${copy.location}. ${left} ${left === 1 ? "copy remains" : "copies remain"}.` : `Uninstalled ${name}.`,
    });
  } catch (error) {
    if (mine()) notice.set({ kind: "error", message: failureText(error, "The Plugin could not be uninstalled.") });
  } finally {
    if (mine()) preparing.set("");
  }
}
