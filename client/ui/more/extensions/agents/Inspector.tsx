import { computed, Image, openUrl, Scroll, signal, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button, TextView } from "nsh/base";
import { pluginSkillEntries, type PluginSkillEntry } from "../../../../core/extensions/pluginSkillsDirectory";
import { pluginAgentLabel, type InstalledPluginCopy } from "../../../../core/extensions/pluginsManagement";
import { pluginCopyLabel, pluginReadonlyReason, type LogicalPlugin } from "../../../../core/extensions/pluginsScreenModel";
import { skillFileLink, skillImagePath } from "../../../../core/extensions/skillImagePath";
import { resolveSkillCopyPluginOwner, skillPluginStatusReason } from "../../../../core/extensions/skillsPluginOwnership";
import { buildSkillFileTree, defaultSkillFile, skillCopyLocation, skillMarkdownBody, skillRenderer, type LogicalSkill, type SkillTreeNode } from "../../../../core/extensions/skillsScreenModel";
import { skillRowSupportsDelete } from "../../../../core/extensions/skillsSurfaceModel";
import { scopeLabel, type InstalledSkill, type ManagedSkillAgent, type PackageDetail } from "../../../../core/extensions/skillsManagement";
import { failureText } from "../../../../core/more/daemon";
import { Icon, type IconName } from "../../../kit/Icon";
import { EmptyState, Notice, Spinner, Tag } from "../../../kit/kit";
import { AgentMarks } from "./marks";
import { cachedReader, SkillImage, SkillImageViewer, type ImageReader } from "./SkillImages";
import { allSkills, inspectState, mutationOperations, preparing, readSkillCopy } from "./state";

/** A titled block inside a sheet. */
export function Block(props: { title: string; children?: JSX.Element }) {
  return (
    <View class="mt-5 gap-2">
      <Text class="text-sub font-semibold text-muted-foreground" role="header">{props.title}</Text>
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
        {props.detail ? <Text class="text-sub text-muted-foreground line-clamp-1">{props.detail}</Text> : null}
        {props.children ?? null}
      </View>
      <Button testID="sheet-close" accessibilityLabel={props.closeLabel} onPress={props.onClose} class="size-11 items-center justify-center rounded-full active:bg-accent">
        <Icon name="close" size={20} />
      </Button>
    </View>
  );
}

/** A full-width button at the end of a sheet: what this copy's removal does, then the removal. */
function Lifecycle(props: { sentence: string; label: string; busyLabel?: string; accessibilityLabel: string; busy: boolean; disabled: boolean; testID: string; onPress: () => void }) {
  return (
    <View class="mt-6 gap-2 border-t border-border pt-4">
      <Text class="text-sub text-muted-foreground">{props.sentence}</Text>
      <Button
        testID={props.testID}
        accessibilityLabel={props.accessibilityLabel}
        disabled={props.disabled || props.busy}
        onPress={props.onPress}
        class="h-[48px] flex-row items-center justify-center gap-2 rounded-(--radius-control) bg-muted px-4 active:opacity-70 disabled:opacity-40"
      >
        {props.busy ? <Spinner size={14} /> : <Icon name="trash" size={16} tone="seal" />}
        <Text class="text-body font-semibold text-destructive">{props.busy && props.busyLabel ? props.busyLabel : props.label}</Text>
      </Button>
    </View>
  );
}

/** Why a copy stays: a lock, "Protected", the reason, and the way to its Plugin when one provides it. */
function Protected(props: { reason: string; children?: JSX.Element }) {
  return (
    <View testID="protected" class="mt-6 gap-2 border-t border-border pt-4">
      <View class="flex-row items-center gap-1.5">
        <Icon name="lock" size={15} />
        <Text class="text-sub font-semibold">Protected</Text>
      </View>
      <Text class="text-sub text-muted-foreground">{props.reason}</Text>
      {props.children ?? null}
    </View>
  );
}

type FlatNode = { node: SkillTreeNode; depth: number };

function visibleNodes(nodes: SkillTreeNode[], expanded: ReadonlySet<string>, depth = 0): FlatNode[] {
  return nodes.flatMap((node) => [{ node, depth }, ...(node.kind === "directory" && expanded.has(node.path) ? visibleNodes(node.children, expanded, depth + 1) : [])]);
}

/** The folders along a file's path, so the file shows when its copy opens. */
function ancestors(path: string | undefined): Set<string> {
  const parts = path?.split("/").slice(0, -1) ?? [];
  return new Set(parts.map((_, index) => parts.slice(0, index + 1).join("/")));
}

function Code(props: { content: string }) {
  return (
    <Scroll keyboardShouldPersistTaps="handled" horizontal class="rounded-(--radius-control) bg-background">
      <View selectable class="p-3"><Text class="font-mono text-code">{props.content || "This file is empty."}</Text></View>
    </Scroll>
  );
}

/**
 * The files of one Skill copy and the one shown: a tree whose folders open and
 * close, Markdown rendered (its pictures and its links to other files of the
 * same copy, nothing outside it), JSON pretty-printed, pictures as pictures that
 * open larger among the copy's others, a binary file named but not shown.
 * Made once per copy, so the open folders stay while files are picked.
 */
export function FileBrowser(props: { detail: PackageDetail; loading: boolean; error?: string; onSelectFile: (path: string) => void; readFile: (path: string) => Promise<PackageDetail> }) {
  const files = () => props.detail.files ?? [];
  const selected = () => props.detail.preview?.path ?? defaultSkillFile(files());
  const expanded = signal<ReadonlySet<string>>(ancestors(untracked(selected)));
  const viewing = signal<string | null>(null);
  // The width of the rendered SKILL.md, which its pictures take.
  const markdownWidth = signal(0);
  const copyId = untracked(() => props.detail.copyId);
  const read: ImageReader = cachedReader(async (path) => {
    const preview = untracked(() => props.detail.preview);
    if (preview?.path === path && preview.dataUrl) return preview.dataUrl;
    const next = await props.readFile(path);
    if (next.copyId !== copyId || next.preview?.path !== path || !next.preview.dataUrl) throw new Error(next.preview?.notice || "This Skill image is unavailable.");
    return next.preview.dataUrl;
  });
  const images = () => files().filter((file) => file.mediaType.startsWith("image/")).map((file) => file.path);
  const preview = () => props.detail.preview;
  const renderer = () => {
    const p = preview();
    return p && !p.dataUrl ? skillRenderer(p.kind, p.content) : null;
  };
  const toggle = (path: string) => {
    const next = new Set(expanded());
    if (next.has(path)) next.delete(path);
    else next.add(path);
    expanded.set(next);
  };
  const markdownPath = () => preview()?.path || "SKILL.md";
  const openLink = (url: string) => {
    const target = skillFileLink(url, markdownPath(), files());
    if (target) props.onSelectFile(target);
    else if (/^https?:\/\//i.test(url)) void openUrl(url);
  };
  return (
    <View class="gap-2">
      {files().length ? (
        <View class="overflow-hidden rounded-(--radius-group) bg-sheet-surface">
          {visibleNodes(buildSkillFileTree(files()), expanded()).map(({ node, depth }) =>
            node.kind === "directory" ? (
              <View
                role="button"
                testID={`skill-folder-${node.path}`}
                accessibilityLabel={node.name}
                accessibilityState={{ expanded: expanded().has(node.path) }}
                onPress={() => toggle(node.path)}
                class="flex-row items-center gap-2 py-2 pr-3 active:bg-accent"
                style={{ paddingLeft: 12 + depth * 14 }}
              >
                <Icon name={expanded().has(node.path) ? "folder-open" : "folder"} size={16} />
                <Text class="flex-1 text-sub text-muted-foreground line-clamp-1">{node.name}</Text>
              </View>
            ) : (
              <Button
                testID={`skill-file-${node.path}`}
                selected={selected() === node.path}
                onPress={() => props.onSelectFile(node.path)}
                class={`flex-row items-center gap-2 py-2 pr-3 active:bg-accent ${selected() === node.path ? "bg-secondary" : ""}`}
                style={{ paddingLeft: 12 + depth * 14 }}
              >
                <Icon name={node.file?.kind === "binary" ? "attach" : "document-text"} size={16} />
                <Text class="flex-1 text-sub line-clamp-1">{node.name}</Text>
              </Button>
            ),
          )}
        </View>
      ) : (
        <Text class="text-sub text-muted-foreground">This copy contains no readable files.</Text>
      )}
      <View class="mt-1 gap-2 border-t border-border pt-3">
        <Text class="font-mono text-code text-muted-foreground line-clamp-1">{selected() || "No file selected"}</Text>
        {props.loading ? (
          <View class="flex-row items-center gap-2">
            <Spinner size={14} />
            <Text class="text-sub text-muted-foreground">Loading file</Text>
          </View>
        ) : null}
        {props.error ? <Notice tone="failed" title="File unavailable" detail={props.error} /> : null}
        {preview()?.notice ? <Notice title={preview()!.notice!} /> : null}
        {preview()?.dataUrl ? (
          <View role="button" testID="skill-file-image" accessibilityLabel={preview()!.path} accessibilityHint="Opens the image" onPress={() => viewing.set(preview()!.path)} class="active:opacity-80">
            <Image src={preview()!.dataUrl!} class="h-48 w-full rounded-[14px]" style={{ objectFit: "contain" }} accessibilityLabel={preview()!.path} />
          </View>
        ) : null}
        {preview()?.status === "binary" ? (
          <EmptyState icon="attach" title="Binary file" detail={`${preview()!.mediaType} · ${preview()!.size} bytes. Content preview is unavailable.`} />
        ) : null}
        {renderer() === "markdown" ? (
          <View onLayout={({ bounds }) => markdownWidth.set(bounds.size.width)}>
          <TextView
            testID="skill-markdown"
            markdown={skillMarkdownBody(preview()?.content ?? "") || "This file is empty."}
            selectable
            class="text-body"
            onLinkPress={(event) => openLink(event.url)}
            renderImage={(image) => {
              let path: string | null = null;
              let problem = "";
              try {
                path = skillImagePath(image.src, markdownPath(), files());
              } catch (reason) {
                problem = failureText(reason, "This Skill image is unavailable.");
              }
              return <SkillImage path={path} alt={image.alt} problem={problem} read={read} onOpen={(target) => viewing.set(target)} width={markdownWidth} />;
            }}
          />
          </View>
        ) : null}
        {renderer() === "text" ? <Code content={preview()?.content ?? ""} /> : null}
        {renderer() === "json" ? <Code content={JSON.stringify(JSON.parse(preview()?.content ?? "null"), null, 2)} /> : null}
        {renderer() === "invalid-json" ? (
          <View class="gap-2">
            <Notice title="Invalid JSON; showing original text." />
            <Code content={preview()?.content ?? ""} />
          </View>
        ) : null}
        {preview()?.status === "truncated" ? <Text class="text-caption text-muted-foreground">{`Showing the first ${preview()!.bytesReturned} of ${preview()!.size} bytes.`}</Text> : null}
      </View>
      <SkillImageViewer images={images()} start={viewing()} read={read} onClose={() => viewing.set(null)} />
    </View>
  );
}

/** One copy of a logical Skill in the inspector: a tap shows that copy; the shown one is marked. */
function SkillCopyRow(props: { copy: InstalledSkill; selected: boolean; onInspect: (copy: InstalledSkill) => void; onDelete: (copy: InstalledSkill) => void }) {
  const location = () => skillCopyLocation(props.copy);
  const deletable = () => skillRowSupportsDelete(props.copy, mutationOperations());
  return (
    <View class={`flex-row items-center rounded-(--radius-control) ${props.selected ? "bg-accent" : "bg-sheet-surface"}`}>
      <Button
        testID={`inspect-copy-${props.copy.id}`}
        accessibilityLabel={location().label}
        selected={props.selected}
        onPress={() => props.onInspect(props.copy)}
        class="min-w-0 flex-1 flex-row items-center gap-2 py-2 pl-3 pr-1 active:opacity-70"
      >
        <View class="min-w-0 flex-1 gap-0.5">
          <AgentMarks agents={props.copy.agents as ManagedSkillAgent[]} size={16} />
          <Text class="text-sub line-clamp-2">{location().label}</Text>
          <Text class="font-mono text-code text-muted-foreground line-clamp-1">{location().path}</Text>
        </View>
        {props.selected ? null : <Icon name="chevron-right" size={16} tone="faint" />}
      </Button>
      {deletable() ? (
        <Button
          testID={`delete-copy-${props.copy.id}`}
          accessibilityLabel={`Delete ${props.copy.name} from ${location().label}`}
          disabled={Boolean(preparing())}
          onPress={() => props.onDelete(props.copy)}
          class="mr-1 size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-50"
        >
          {preparing() === `delete:${props.copy.id}` ? <Spinner size={16} /> : <Icon name="trash" size={18} tone="seal" />}
        </Button>
      ) : null}
    </View>
  );
}

/**
 * Skill details: who provides it, whether it can be deleted here, its files,
 * the agents it serves, each copy when there are several (a tap shows that
 * copy), and the delete of the copy shown, or why it is protected.
 */
export function SkillInspector(props: {
  logical: LogicalSkill | null;
  copy: InstalledSkill;
  plugins: readonly LogicalPlugin[];
  pluginsShown: boolean;
  onSelectFile: (path: string) => void;
  onInspect: (copy: InstalledSkill) => void;
  onDelete: (copy: InstalledSkill) => void;
  onOpenPlugin: (key: string) => void;
  onClose: () => void;
}) {
  const state = () => inspectState();
  const detail = () => (state().data?.copyId === props.copy.id ? state().data : undefined);
  // The detail's own word on the Plugin counts as much as the inventory's.
  const owned = () => ({ ...props.copy, plugin: detail()?.plugin ?? props.copy.plugin });
  const owner = () => resolveSkillCopyPluginOwner(owned(), props.plugins);
  const canDelete = () => {
    const d = detail();
    return skillRowSupportsDelete(props.copy, mutationOperations()) && (!d || (d.capability.canDelete && !d.plugin)) && !owner();
  };
  const location = () => skillCopyLocation(props.copy);
  const deleting = () => preparing() === `delete:${props.copy.id}`;
  // The body is made again only for another copy, so a picked file keeps the open folders.
  const shown = computed(() => detail()?.copyId ?? null, { equals: (a, b) => a === b });
  const body = (): JSX.Element => (
    <View>
      <Block title="Overview">
        {detail()?.description ? <Text class="text-body">{detail()!.description!}</Text> : null}
        <View class="flex-row items-center gap-2">
          <Tag label={detail()?.enabled ? "Enabled" : "Disabled"} />
          <Text class="text-sub text-muted-foreground">{scopeLabel(detail()?.scope ?? props.copy.scope)}</Text>
        </View>
        {(detail()?.warnings ?? []).map((warning) => <Text class="text-sub text-warning">{warning}</Text>)}
      </Block>
      <Block title="Files">
        <FileBrowser
          detail={detail()!}
          loading={state().status === "loading"}
          error={state().status === "error" ? state().error : undefined}
          onSelectFile={props.onSelectFile}
          readFile={(path) => readSkillCopy(props.copy, path)}
        />
      </Block>
      <Block title="Available to">
        <AgentMarks agents={(detail()?.agents ?? []) as ManagedSkillAgent[]} size={20} showLabels />
      </Block>
      {props.logical && props.logical.copies.length > 1 ? (
        <Block title={`Locations (${props.logical.copies.length})`}>
          {props.logical.copies.map((copy) => <SkillCopyRow copy={copy} selected={copy.id === props.copy.id} onInspect={props.onInspect} onDelete={props.onDelete} />)}
        </Block>
      ) : null}
      {canDelete() ? (
        <Lifecycle
          testID="inspector-delete"
          sentence={`Permanently delete this copy from ${location().label}.`}
          label="Delete Skill"
          busyLabel="Deleting..."
          accessibilityLabel={`Delete ${props.copy.name} from ${location().label}`}
          busy={deleting()}
          disabled={Boolean(preparing())}
          onPress={() => props.onDelete(props.copy)}
        />
      ) : (
        <Protected reason={skillPluginStatusReason(owned(), owner())}>
          {owner() && props.pluginsShown ? (
            <Button
              testID="open-skill-plugin"
              accessibilityLabel={`Open the ${owner()!.displayName} Plugin that provides this Skill`}
              onPress={() => props.onOpenPlugin(owner()!.key)}
              class="h-[48px] flex-row items-center justify-center gap-2 rounded-(--radius-control) bg-muted px-4 active:opacity-70"
            >
              <Icon name="puzzle" size={16} />
              <Text class="text-body font-semibold">{`Open ${owner()!.displayName}`}</Text>
            </Button>
          ) : null}
        </Protected>
      )}
    </View>
  );
  return (
    <View testID="skill-inspector" class="gap-1">
      <SheetHeader title={detail()?.skillName ?? props.copy.name} detail={location().label} closeLabel="Close Skill details" onClose={props.onClose}>
        <View class="flex-row flex-wrap gap-1.5">
          <Tag label={owner() ? `From ${owner()!.displayName}` : props.copy.scope === "builtin" ? "Built-in" : "Standalone"} />
          <Tag label={canDelete() ? "Can delete" : "Protected"} />
        </View>
      </SheetHeader>
      {state().status === "error" && !detail() ? (
        <EmptyState testID="skill-unavailable" icon="warning" title="Skill unavailable" detail={state().error} action="Try again" onAction={() => props.onInspect(props.copy)} />
      ) : null}
      {state().status === "loading" && !detail() ? <EmptyState busy title="Loading Skill" /> : null}
      {((id) => untracked(() => (id ? body() : null)))(shown())}
    </View>
  );
}

/** A Skill a Plugin provides, opened in place inside the Plugin's details. */
function PluginSkillEntryView(props: { entry: PluginSkillEntry }) {
  const open = signal(false);
  const detail = signal<PackageDetail | null>(null);
  const error = signal("");
  const loading = signal(false);
  let token = 0;
  const read = (path?: string) => {
    const copy = props.entry.copy;
    if (!copy) return;
    const mine = ++token;
    loading.set(true);
    readSkillCopy(copy, path)
      .then((next) => { if (mine === token) { detail.set(next); error.set(""); } })
      .catch((reason: unknown) => { if (mine === token) error.set(failureText(reason, "Could not inspect this Skill.")); })
      .finally(() => { if (mine === token) loading.set(false); });
  };
  const shown = computed(() => detail()?.copyId ?? null, { equals: (a, b) => a === b });
  return (
    <View class="rounded-(--radius-group) bg-sheet-surface">
      <View
        role="button"
        testID={`plugin-skill-${props.entry.name}`}
        accessibilityLabel={`Toggle ${props.entry.name} Skill details`}
        accessibilityState={{ expanded: open() }}
        onPress={() => {
          open.set(!open());
          if (open() && !detail()) read();
        }}
        class="flex-row items-center gap-2 px-3 py-2.5 active:bg-accent"
      >
        <Icon name={open() ? "chevron-down" : "chevron-right"} size={15} tone="faint" />
        <Icon name="book" size={16} tone="warning" />
        <View class="min-w-0 flex-1">
          <Text class="text-sub line-clamp-1">{props.entry.name}</Text>
          {props.entry.path ? <Text class="font-mono text-code text-muted-foreground line-clamp-1">{props.entry.path}</Text> : null}
        </View>
        {props.entry.copy ? null : <Icon name="help" size={16} tone="faint" />}
      </View>
      <View class="flex-col px-3 pb-3" style={{ display: open() ? "flex" : "none" }}>
        {!props.entry.copy ? (
          <Text class="text-sub text-muted-foreground">This Skill copy was not found in the current Skills inventory, so its files cannot be shown here.</Text>
        ) : null}
        {error() && !detail() ? <Text class="text-sub text-warning">{error()}</Text> : null}
        {loading() && !detail() ? (
          <View class="flex-row items-center gap-2">
            <Spinner size={14} />
            <Text class="text-sub text-muted-foreground">Loading Skill</Text>
          </View>
        ) : null}
        {((id) =>
          untracked(() =>
            id ? <FileBrowser detail={detail()!} loading={loading()} error={error() || undefined} onSelectFile={(path) => read(path)} readFile={(path) => readSkillCopy(props.entry.copy!, path)} /> : null,
          ))(shown())}
      </View>
    </View>
  );
}

function ChoosableCopy(props: { label: string; testID: string; selected: boolean; onPress: () => void; children?: JSX.Element }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.label} selected={props.selected} onPress={props.onPress} class="min-w-0 flex-1 flex-row active:opacity-70">
      {props.children}
    </Button>
  );
}

/** One copy of a Plugin: where it is, which agents have it, and its uninstall when it can go. A tap selects it, where it can be. */
export function PluginCopyCard(props: { copy: InstalledPluginCopy; selected?: boolean; onChoose?: () => void; onUninstall: (copy: InstalledPluginCopy) => void }) {
  const content = () => (
    <View class="min-w-0 flex-1 gap-0.5">
      <Text class="text-sub">{pluginCopyLabel(props.copy)}</Text>
      <AgentMarks agents={props.copy.agents.filter(isManaged)} size={16} />
      <Text class="font-mono text-code text-muted-foreground line-clamp-1">{props.copy.rootPath}</Text>
      {props.copy.capability.canUninstall ? null : <Text class="text-caption text-muted-foreground">{pluginReadonlyReason(props.copy)}</Text>}
    </View>
  );
  return (
    <View class={`flex-row items-center gap-1 rounded-(--radius-control) px-3 py-2 ${props.selected ? "bg-accent" : "bg-sheet-surface"}`}>
      {props.onChoose ? <ChoosableCopy label={`Select ${pluginCopyLabel(props.copy)} copy`} testID={`select-copy-${props.copy.copyId}`} selected={!!props.selected} onPress={() => props.onChoose?.()}>{content()}</ChoosableCopy> : content()}
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

function componentIcon(kind: string): IconName {
  switch (kind) {
    case "skill": return "book";
    case "agent": return "people";
    case "command": return "terminal";
    case "hook": return "git-branch";
    case "mcp": return "server";
    case "app": return "apps";
    default: return "document";
  }
}

/**
 * Plugin details: what it is, the Skills it brings, its parts, where it lives,
 * and its uninstall. With several copies, the selected one decides the parts,
 * the agents, the place and what Uninstall removes.
 */
export function PluginDetails(props: { plugin: LogicalPlugin; onUninstall: (copy: InstalledPluginCopy) => void; onClose: () => void }) {
  const chosen = signal<string | null>(null);
  const copy = () => props.plugin.copies.find((candidate) => candidate.copyId === chosen()) ?? props.plugin.copies[0]!;
  const entries = () => pluginSkillEntries(props.plugin, allSkills());
  const others = () => copy().components.filter((component) => component.kind !== "skill");
  const managed = () => copy().agents.filter(isManaged);
  const unmanaged = () => copy().agents.filter((agent) => !isManaged(agent));
  return (
    <View testID="plugin-details" class="gap-1">
      <SheetHeader title={props.plugin.displayName} detail={pluginCopyLabel(copy())} closeLabel="Close Plugin details" onClose={props.onClose}>
        <View class="flex-row flex-wrap gap-1.5">
          <Tag label={copy().capability.canUninstall ? "Can uninstall" : "Protected"} />
          {props.plugin.versions.length ? <Tag label={`v${props.plugin.versions.join(", v")}`} /> : null}
        </View>
      </SheetHeader>
      {copy().description || props.plugin.description ? (
        <Block title="Overview"><Text class="text-body">{copy().description || props.plugin.description || ""}</Text></Block>
      ) : null}
      {entries().length ? (
        <Block title={`Skills (${entries().length})`}>
          {entries().map((entry) => <PluginSkillEntryView entry={entry} />)}
        </Block>
      ) : null}
      {others().length ? (
        <Block title="Components">
          {others().map((component) => (
            <View class="flex-row items-start gap-2">
              <Icon name={componentIcon(component.kind)} size={18} />
              <View class="min-w-0 flex-1">
                <Text class="text-sub">{component.name}</Text>
                {component.path ? <Text class="font-mono text-code text-muted-foreground line-clamp-1">{component.path}</Text> : null}
              </View>
            </View>
          ))}
        </Block>
      ) : null}
      <Block title="Available to">
        {managed().length || !unmanaged().length ? <AgentMarks agents={managed()} size={20} showLabels /> : null}
        {unmanaged().length ? <Text class="text-sub">{unmanaged().map(pluginAgentLabel).join(", ")}</Text> : null}
      </Block>
      {props.plugin.copies.length > 1 ? (
        <Block title={`Copies (${props.plugin.copies.length})`}>
          {props.plugin.copies.map((candidate) => (
            <PluginCopyCard copy={candidate} selected={candidate.copyId === copy().copyId} onChoose={() => chosen.set(candidate.copyId)} onUninstall={props.onUninstall} />
          ))}
        </Block>
      ) : (
        <Block title="Location">
          <Text class="text-sub">{copy().location}</Text>
          <View selectable><Text class="font-mono text-code text-muted-foreground">{copy().rootPath}</Text></View>
        </Block>
      )}
      {copy().capability.canUninstall ? (
        <Lifecycle
          testID="uninstall-plugin"
          sentence={`Permanently removes only this exact copy from ${copy().location}.`}
          label="Uninstall Plugin"
          accessibilityLabel={`Uninstall ${props.plugin.displayName} from ${copy().location}`}
          busy={preparing() === `uninstall:${copy().copyId}`}
          disabled={Boolean(preparing())}
          onPress={() => props.onUninstall(copy())}
        />
      ) : (
        <Protected reason={pluginReadonlyReason(copy())} />
      )}
    </View>
  );
}
