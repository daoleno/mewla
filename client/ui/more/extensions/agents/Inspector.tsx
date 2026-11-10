import { Image, Scroll, signal, Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { pluginSkillEntries, type PluginSkillEntry } from "../../../../core/extensions/pluginSkillsDirectory";
import { pluginAgentLabel, type InstalledPluginCopy } from "../../../../core/extensions/pluginsManagement";
import { pluginCopyLabel, pluginReadonlyReason, type LogicalPlugin } from "../../../../core/extensions/pluginsScreenModel";
import { resolveSkillCopyPluginOwner, skillPluginStatusReason } from "../../../../core/extensions/skillsPluginOwnership";
import { buildSkillFileTree, defaultSkillFile, skillCopyLocation, skillRenderer, type LogicalSkill, type SkillTreeNode } from "../../../../core/extensions/skillsScreenModel";
import { skillRowSupportsDelete } from "../../../../core/extensions/skillsSurfaceModel";
import { scopeLabel, type InstalledSkill, type ManagedSkillAgent, type PackageDetail } from "../../../../core/extensions/skillsManagement";
import { failureText } from "../../../../core/more/daemon";
import { Icon } from "../../kit/Icon";
import { EmptyState, Notice, PillButton, Spinner, Tag } from "../../kit/kit";
import { AgentMarks } from "./marks";
import { allSkills, inspectState, mutationOperations, preparing, readSkillCopy } from "./state";

/** A titled block inside a sheet. */
export function Block(props: { title: string; children?: JSX.Element }) {
  return (
    <View class="mt-5 gap-2">
      <Text class="text-sm font-medium text-muted-foreground" role="header">{props.title}</Text>
      {props.children}
    </View>
  );
}

/** The sheet header: a title over a line, with Close. */
export function SheetHeader(props: { title: string; detail?: string; closeLabel: string; onClose: () => void; children?: JSX.Element }) {
  return (
    <View class="flex-row items-start gap-3">
      <View class="min-w-0 flex-1 gap-1">
        <Text class="text-xl font-semibold" role="header">{props.title}</Text>
        {props.detail ? <Text class="text-sm text-muted-foreground">{props.detail}</Text> : null}
        {props.children ?? null}
      </View>
      <Button testID="sheet-close" accessibilityLabel={props.closeLabel} onPress={props.onClose} class="size-11 items-center justify-center rounded-full active:bg-accent">
        <Icon name="close" size={20} />
      </Button>
    </View>
  );
}

function flatten(nodes: SkillTreeNode[], depth = 0): Array<{ node: SkillTreeNode; depth: number }> {
  return nodes.flatMap((node) => [{ node, depth }, ...flatten(node.children, depth + 1)]);
}

function Code(props: { content: string }) {
  return (
    <Scroll horizontal class="rounded-xl border border-border bg-background">
      <Text class="p-3 font-mono text-xs">{props.content || "This file is empty."}</Text>
    </Scroll>
  );
}

/**
 * The files of one Skill copy and the one shown: Markdown and text as they are,
 * JSON pretty-printed, pictures as pictures, a binary file named but not shown.
 */
export function FileBrowser(props: { detail: PackageDetail; loading: boolean; error?: string; onSelectFile: (path: string) => void }) {
  const files = () => props.detail.files ?? [];
  const selected = () => props.detail.preview?.path ?? defaultSkillFile(files());
  const preview = () => props.detail.preview;
  const renderer = () => {
    const p = preview();
    return p && !p.dataUrl ? skillRenderer(p.kind, p.content) : null;
  };
  return (
    <View class="gap-2">
      <View class="overflow-hidden rounded-xl border border-border">
        {flatten(buildSkillFileTree(files())).map(({ node, depth }) =>
          node.kind === "directory" ? (
            <View class="flex-row items-center gap-2 py-2 pr-3" style={{ paddingLeft: 12 + depth * 14 }}>
              <Icon name="folder" size={16} />
              <Text class="text-sm text-muted-foreground">{node.name}</Text>
            </View>
          ) : (
            <Button
              testID={`skill-file-${node.path}`}
              selected={selected() === node.path}
              onPress={() => props.onSelectFile(node.path)}
              class={`flex-row items-center gap-2 py-2 pr-3 active:bg-accent ${selected() === node.path ? "bg-secondary" : ""}`}
              style={{ paddingLeft: 12 + depth * 14 }}
            >
              <Icon name={node.file?.kind === "binary" ? "document" : "document-text"} size={16} />
              <Text class="flex-1 text-sm line-clamp-1">{node.name}</Text>
            </Button>
          ),
        )}
      </View>
      <View class="flex-row items-center gap-2">
        <Text class="flex-1 font-mono text-xs text-muted-foreground line-clamp-1">{selected() || "No file selected"}</Text>
        {props.loading ? <Spinner size={14} /> : null}
      </View>
      {props.error ? <Notice tone="failed" title="File unavailable" detail={props.error} /> : null}
      {preview()?.notice ? <Notice title={preview()!.notice!} /> : null}
      {preview()?.dataUrl ? <Image src={preview()!.dataUrl!} class="h-48 w-full rounded-xl" accessibilityLabel={preview()!.path} /> : null}
      {preview()?.status === "binary" ? (
        <EmptyState icon="document" title="Binary file" detail={`${preview()!.mediaType} · ${preview()!.size} bytes. Content preview is unavailable.`} />
      ) : null}
      {renderer() === "markdown" || renderer() === "text" ? <Code content={preview()?.content ?? ""} /> : null}
      {renderer() === "json" ? <Code content={JSON.stringify(JSON.parse(preview()?.content ?? "null"), null, 2)} /> : null}
      {renderer() === "invalid-json" ? (
        <View class="gap-2">
          <Notice title="Invalid JSON; showing original text." />
          <Code content={preview()?.content ?? ""} />
        </View>
      ) : null}
      {preview()?.status === "truncated" ? <Text class="text-xs text-muted-foreground">{`Showing the first ${preview()!.bytesReturned} of ${preview()!.size} bytes.`}</Text> : null}
    </View>
  );
}

/** One copy of a logical Skill, with its delete when this copy can be deleted here. */
function SkillCopyRow(props: { copy: InstalledSkill; onDelete: (copy: InstalledSkill) => void }) {
  const location = () => skillCopyLocation(props.copy);
  const deletable = () => skillRowSupportsDelete(props.copy, mutationOperations());
  return (
    <View class="flex-row items-center gap-2 rounded-xl border border-border px-3 py-2">
      <View class="min-w-0 flex-1">
        <Text class="text-sm">{location().label}</Text>
        <Text class="font-mono text-xs text-muted-foreground line-clamp-1">{location().path}</Text>
        {deletable() ? null : <Text class="text-xs text-muted-foreground">This copy cannot be deleted from here.</Text>}
      </View>
      {deletable() ? (
        <PillButton
          testID={`delete-copy-${props.copy.id}`}
          title={preparing() === `delete:${props.copy.id}` ? "Deleting..." : "Delete Skill"}
          destructive
          busy={preparing() === `delete:${props.copy.id}`}
          disabled={Boolean(preparing())}
          onPress={() => props.onDelete(props.copy)}
        />
      ) : null}
    </View>
  );
}

/**
 * Skill details: who provides it, whether it can be deleted here, its files,
 * the agents it serves, and each copy when there are several.
 */
export function SkillInspector(props: {
  logical: LogicalSkill | null;
  copy: InstalledSkill;
  plugins: readonly LogicalPlugin[];
  pluginsShown: boolean;
  onSelectFile: (path: string) => void;
  onDelete: (copy: InstalledSkill) => void;
  onOpenPlugin: (key: string) => void;
  onClose: () => void;
}) {
  const owner = () => resolveSkillCopyPluginOwner(props.copy, props.plugins);
  const deletable = () => skillRowSupportsDelete(props.copy, mutationOperations());
  const state = () => inspectState();
  const detail = () => (state().data?.copyId === props.copy.id ? state().data : undefined);
  return (
    <View testID="skill-inspector" class="gap-1">
      <SheetHeader title={props.copy.name} closeLabel="Close Skill details" onClose={props.onClose}>
        <View class="flex-row flex-wrap gap-1.5">
          <Tag label={owner() ? `From ${owner()!.displayName}` : "Standalone"} />
          <Tag label={deletable() ? "Can delete" : "Protected"} />
          <Tag label={props.copy.enabled ? "Enabled" : "Disabled"} />
        </View>
      </SheetHeader>
      {state().status === "error" && !detail() ? <EmptyState icon="warning" title="Skill unavailable" detail={state().error} /> : null}
      {state().status === "loading" && !detail() ? <EmptyState busy title="Loading Skill" /> : null}
      {detail() ? (
        <View>
          <Block title="Overview">
            {detail()!.description ? <Text class="text-base">{detail()!.description!}</Text> : null}
            <Text class="text-sm text-muted-foreground">{`${scopeLabel(detail()!.scope)} · ${detail()!.location}`}</Text>
            {deletable() ? null : <Text class="text-sm text-muted-foreground">{skillPluginStatusReason(props.copy, owner())}</Text>}
            {(detail()!.warnings ?? []).map((warning) => <Text class="text-sm text-warning">{warning}</Text>)}
            {owner() && props.pluginsShown ? (
              <PillButton
                testID="open-skill-plugin"
                title={`Open ${owner()!.displayName}`}
                onPress={() => props.onOpenPlugin(owner()!.key)}
              />
            ) : null}
          </Block>
          <Block title="Files">
            <FileBrowser detail={detail()!} loading={state().status === "loading"} error={state().status === "error" ? state().error : undefined} onSelectFile={props.onSelectFile} />
          </Block>
          <Block title="Available to">
            <AgentMarks agents={detail()!.agents} size={20} />
          </Block>
          {props.logical && props.logical.copies.length > 1 ? (
            <Block title={`Locations (${props.logical.copies.length})`}>
              {props.logical.copies.map((copy) => <SkillCopyRow copy={copy} onDelete={props.onDelete} />)}
            </Block>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

/** A Skill a Plugin provides, opened in place inside the Plugin's details. */
function PluginSkillEntryView(props: { entry: PluginSkillEntry }) {
  const open = signal(false);
  const detail = signal<PackageDetail | null>(null);
  const error = signal("");
  const loading = signal(false);
  const read = (path?: string) => {
    const copy = props.entry.copy;
    if (!copy) return;
    loading.set(true);
    readSkillCopy(copy, path)
      .then((next) => { detail.set(next); error.set(""); })
      .catch((reason: unknown) => error.set(failureText(reason, "Could not inspect this Skill.")))
      .finally(() => loading.set(false));
  };
  return (
    <View class="rounded-xl border border-border">
      <Button
        testID={`plugin-skill-${props.entry.name}`}
        disabled={!props.entry.copy}
        onPress={() => {
          open.set(!open());
          if (open() && !detail()) read();
        }}
        class="flex-row items-center gap-2 px-3 py-2.5 active:bg-accent"
      >
        <Icon name="skills" size={16} />
        <View class="min-w-0 flex-1">
          <Text class="text-sm">{props.entry.name}</Text>
          {props.entry.copy ? null : <Text class="text-xs text-muted-foreground">Skill copy unavailable</Text>}
        </View>
        {props.entry.copy ? <Icon name={open() ? "chevron-up" : "chevron-down"} size={16} tone="faint" /> : null}
      </Button>
      {open() ? (
        <View class="px-3 pb-3">
          {error() && !detail() ? <Text class="text-sm text-destructive">{error()}</Text> : null}
          {loading() && !detail() ? <Spinner /> : null}
          {detail() ? <FileBrowser detail={detail()!} loading={loading()} error={error() || undefined} onSelectFile={(path) => read(path)} /> : null}
        </View>
      ) : null}
    </View>
  );
}

function PluginCopyRow(props: { copy: InstalledPluginCopy; onUninstall: (copy: InstalledPluginCopy) => void }) {
  return (
    <View class="flex-row items-center gap-2 rounded-xl border border-border px-3 py-2">
      <View class="min-w-0 flex-1 gap-0.5">
        <Text class="text-sm">{pluginCopyLabel(props.copy)}</Text>
        <AgentMarks agents={props.copy.agents.filter(isManaged)} size={16} />
        <Text class="font-mono text-xs text-muted-foreground line-clamp-1">{props.copy.rootPath}</Text>
        {props.copy.capability.canUninstall ? null : <Text class="text-xs text-muted-foreground">{pluginReadonlyReason(props.copy)}</Text>}
      </View>
      {props.copy.capability.canUninstall ? (
        <Button
          testID={`uninstall-copy-${props.copy.copyId}`}
          accessibilityLabel={`Uninstall copy from ${props.copy.location}`}
          disabled={Boolean(preparing())}
          onPress={() => props.onUninstall(props.copy)}
          class="size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-50"
        >
          {preparing() === `uninstall:${props.copy.copyId}` ? <Spinner size={16} /> : <Icon name="trash" size={20} tone="seal" />}
        </Button>
      ) : null}
    </View>
  );
}

const MANAGED = new Set(["codex", "claude-code", "cursor", "grok", "opencode", "pi"]);
export function isManaged(agent: string): agent is ManagedSkillAgent {
  return MANAGED.has(agent);
}

/** Plugin details: what it is, the Skills it brings, its parts, where it lives, and its uninstall. */
export function PluginDetails(props: { plugin: LogicalPlugin; onUninstall: (copy: InstalledPluginCopy) => void; onClose: () => void }) {
  const copy = () => props.plugin.copies[0]!;
  const entries = () => pluginSkillEntries(props.plugin, allSkills());
  const others = () => copy().components.filter((component) => component.kind !== "skill");
  return (
    <View testID="plugin-details" class="gap-1">
      <SheetHeader title={props.plugin.displayName} detail="Plugin details" closeLabel="Close Plugin details" onClose={props.onClose}>
        <View class="flex-row flex-wrap gap-1.5">
          <Tag label={props.plugin.canUninstall ? "Can uninstall" : "Protected"} />
          {props.plugin.versions.length ? <Tag label={`v${props.plugin.versions.join(", v")}`} /> : null}
        </View>
      </SheetHeader>
      {props.plugin.description ? (
        <Block title="Overview"><Text class="text-base">{props.plugin.description}</Text></Block>
      ) : null}
      {props.plugin.canUninstall ? null : (
        <Text class="mt-3 text-sm text-muted-foreground">{pluginReadonlyReason(copy())}</Text>
      )}
      {entries().length ? (
        <Block title={`Skills (${entries().length})`}>
          {entries().map((entry) => <PluginSkillEntryView entry={entry} />)}
        </Block>
      ) : null}
      {others().length ? (
        <Block title="Components">
          {others().map((component) => (
            <View class="flex-row gap-2">
              <Text class="w-20 text-sm text-muted-foreground">{component.kind}</Text>
              <View class="min-w-0 flex-1">
                <Text class="text-sm">{component.name}</Text>
                {component.path ? <Text class="font-mono text-xs text-muted-foreground line-clamp-1">{component.path}</Text> : null}
              </View>
            </View>
          ))}
        </Block>
      ) : null}
      <Block title="Available to">
        <Text class="text-sm">{props.plugin.agents.map(pluginAgentLabel).join(", ") || "Unknown"}</Text>
      </Block>
      {props.plugin.copies.length > 1 ? (
        <Block title={`Copies (${props.plugin.copies.length})`}>
          {props.plugin.copies.map((candidate) => <PluginCopyRow copy={candidate} onUninstall={props.onUninstall} />)}
        </Block>
      ) : (
        <Block title="Location">
          <Text class="text-sm">{copy().location}</Text>
          <Text class="font-mono text-xs text-muted-foreground">{copy().rootPath}</Text>
          {copy().capability.canUninstall ? (
            <PillButton
              testID="uninstall-plugin"
              title="Uninstall Plugin"
              destructive
              busy={preparing() === `uninstall:${copy().copyId}`}
              disabled={Boolean(preparing())}
              onPress={() => props.onUninstall(copy())}
            />
          ) : null}
        </Block>
      )}
    </View>
  );
}
