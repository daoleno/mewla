import { cleanup, computed, effect, platform, Scroll, signal, Text, untracked, View, windowSize } from "nsh";
import { BackHandler } from "nsh/mobile";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { GuardedSheet as Sheet } from "../../../shell/AppSheet";
import { agentExtensionsScopeLabel, pluginsForScope, skillCopyScope, skillsForScope, type AgentExtensionsScope } from "../../../../core/extensions/agentExtensionsModel";
import type { InstalledPluginCopy } from "../../../../core/extensions/pluginsManagement";
import { filterLogicalPlugins, groupLogicalPlugins, type LogicalPlugin, type PluginCapabilityFilter, type PluginFilters } from "../../../../core/extensions/pluginsScreenModel";
import { skillAgentLabel, skillsRequestData, type InstalledSkill, type ManagedSkillAgent } from "../../../../core/extensions/skillsManagement";
import { filterLogicalSkills, MANAGED_SKILL_AGENTS, skillCopyLocation, type LogicalSkill, type SkillFilters, type SkillScopeFilter, type SkillStatusFilter } from "../../../../core/extensions/skillsScreenModel";
import { skillRowSupportsDelete, type SkillsSurfaceSection } from "../../../../core/extensions/skillsSurfaceModel";
import { daemon, daemonState } from "../../daemon";
import { Icon, type IconName } from "../../../kit/Icon";
import { EmptyState, Notice, Page, PillButton, SearchField, Spinner } from "../../../kit/kit";
import { Segmented } from "../../../kit/Segmented";
import { LeadHero, tile } from "../../../kit/RowTile";
import { AgentIcon } from "../../../icons/AgentIcon";
import type { PageNav } from "../../routes";
import { isManaged, PluginCopyCard, PluginDetails, SheetHeader, SkillInspector } from "./Inspector";
import { AgentMarks } from "./marks";
import {
  allSkills,
  closeInspector,
  deleteSkillCopy,
  followComputer,
  inspected,
  inspectSkill,
  listableSkills,
  mutationOperations,
  notice,
  pluginCopies,
  pluginsState,
  preparing,
  projectCwd,
  refreshAll,
  refreshPlugins,
  refreshSkills,
  skillsState,
  uninstallPluginCopy,
} from "./state";

const SHEET = "max-h-[94%] rounded-t-(--radius-sheet) bg-sheet px-4 pb-8 pt-3 shadow-float";
const BACKDROP = "bg-scrim";
/** From this width, Skill and Plugin details sit beside the list (a tablet, a phone on its side). */
const WIDE_DETAILS = 920;
/** How far a sideways swipe goes before it switches between Plugins and Skills. */
const SWIPE_DISTANCE = 60;

const NO_SKILL_FILTERS: SkillFilters = { agents: [], status: "all", scope: "all" };
const NO_PLUGIN_FILTERS: PluginFilters = { agents: [], capability: "all" };

/**
 * One tool's own plugins and Skills (only Codex and Claude Code have plugins),
 * or the Skills several tools share. Read-only but for delete and uninstall,
 * each of one exact copy, after the computer said what it would do.
 */
export function AgentPage(props: { scope: AgentExtensionsScope; nav: PageNav }) {
  const hasPlugins = props.scope === "codex" || props.scope === "claude-code";
  const chosen = signal<SkillsSurfaceSection | null>(null);
  const skillQuery = signal("");
  const pluginQuery = signal("");
  const skillFilters = signal<SkillFilters>(NO_SKILL_FILTERS);
  const pluginFilters = signal<PluginFilters>(NO_PLUGIN_FILTERS);
  const filtersOpen = signal<SkillsSurfaceSection | null>(null);
  /** The Skill whose copies are offered for delete, the Plugin whose copies for uninstall. */
  const skillPicker = signal<LogicalSkill | null>(null);
  const pluginPicker = signal<LogicalPlugin | null>(null);
  const openPluginKey = signal<string | null>(null);
  let shownComputer: string | null | undefined;

  effect(() => {
    const current = daemon();
    const id = current?.server.id ?? "";
    // Another computer: a copy picked on the last one is never offered to this one.
    if (shownComputer !== undefined && shownComputer !== id) {
      skillPicker.set(null);
      pluginPicker.set(null);
      openPluginKey.set(null);
    }
    shownComputer = id;
    followComputer(id);
    if (current && daemonState() === "connected") untracked(refreshAll);
  });
  // The inspector's copy is kept beside the page: when the page goes with it open, the next
  // agent page would open on it again.
  cleanup(closeInspector);
  // Back closes an open inspector or Plugin before the page goes (the device's Back reached the
  // stack under the sheet).
  onAndroidBack(() => {
    if (wide() || !untracked(panel)) return false;
    if (untracked(panel).startsWith("skill:")) closeInspector();
    else openPluginKey.set(null);
    return true;
  });
  // A new inventory: the copies a picker offered may be gone.
  let pickerInventory: string | undefined;
  effect(() => {
    const generated = skillsRequestData(skillsState())?.generatedAt;
    untracked(() => {
      if (pickerInventory !== undefined && generated !== pickerInventory) skillPicker.set(null);
      pickerInventory = generated;
    });
  });
  // A wide screen shows details beside the list instead of over it.
  const wide = () => windowSize().width >= WIDE_DETAILS;

  const allPlugins = () => groupLogicalPlugins(pluginCopies());
  const plugins = () => pluginsForScope(pluginCopies(), props.scope);
  const skills = () => skillsForScope(listableSkills(), props.scope);
  const pluginOwned = () => allSkills().filter((copy) => skillCopyScope(copy) === props.scope).length - listableSkills().filter((copy) => skillCopyScope(copy) === props.scope).length;
  // A tool opens on its plugins while it has any, otherwise on its Skills.
  const section = (): SkillsSurfaceSection => chosen() ?? (hasPlugins && plugins().length ? "plugins" : "skills");
  const filterAgents = (): ManagedSkillAgent[] => (props.scope === "shared" ? MANAGED_SKILL_AGENTS.filter((agent) => skills().some((skill) => skill.agents.includes(agent))) : []);
  const shownSkills = () => filterLogicalSkills(skills(), skillQuery(), skillFilters());
  const shownPlugins = () => filterLogicalPlugins(plugins(), pluginQuery(), pluginFilters());
  const logicalOf = (copy: InstalledSkill) => skills().find((skill) => skill.copies.some((candidate) => candidate.id === copy.id)) ?? null;
  const openPlugin = () => allPlugins().find((plugin) => plugin.key === openPluginKey()) ?? null;

  const chooseDelete = (skill: LogicalSkill) => {
    if (skill.copies.length > 1) skillPicker.set(skill);
    else void deleteSkillCopy(skill.copies[0]!);
  };
  const chooseUninstall = (plugin: LogicalPlugin) => {
    const uninstallable = plugin.copies.filter((copy) => copy.capability.canUninstall);
    // A protected Plugin is never sent: its details say why.
    if (!uninstallable.length) openPluginKey.set(plugin.key);
    else if (uninstallable.length > 1) pluginPicker.set(plugin);
    else void uninstallPluginCopy(uninstallable[0]!);
  };
  const refresh = () => (section() === "plugins" ? void refreshPlugins() : void refreshSkills());
  const loadingNow = () => (section() === "plugins" ? pluginsState() : skillsState()).status === "loading";

  const projectName = () => projectCwd().replace(/\/+$/, "").split("/").pop() || projectCwd();
  // Plugins and Skills are peer pages: a sideways swipe goes to the other one. In a browser
  // only the tabs do (client/DESIGN.md: no swipe paging on the web), so a drag selects text.
  const swipes = platform.os !== "web";
  const swipe = (dx: number) => {
    if (!hasPlugins || Math.abs(dx) < SWIPE_DISTANCE) return;
    chosen.set(dx < 0 ? "skills" : "plugins");
  };
  // Each is made for the one copy or Plugin it shows (the panel's key), which it keeps while it closes.
  const inspector = () => {
    const copy = untracked(inspected)!;
    return (
      <SkillInspector
        logical={logicalOf(copy)}
        copy={copy}
        plugins={allPlugins()}
        pluginsShown={hasPlugins}
        onSelectFile={(path) => void inspectSkill(copy, path)}
        onInspect={(other) => void inspectSkill(other)}
        onDelete={(other) => void deleteSkillCopy(other)}
        onOpenPlugin={(key) => { closeInspector(); chosen.set("plugins"); openPluginKey.set(key); }}
        onClose={closeInspector}
      />
    );
  };
  const pluginDetails = () => {
    const shown = untracked(openPlugin)!;
    return <PluginDetails plugin={allPlugins().find((plugin) => plugin.key === shown.key) ?? shown} onUninstall={(copy: InstalledPluginCopy) => void uninstallPluginCopy(copy)} onClose={() => openPluginKey.set(null)} />;
  };
  const skillPickerView = (skill: LogicalSkill) => (
    <View testID="skill-copy-picker" class="gap-2">
      <SheetHeader title="Delete copy" detail={skill.name} closeLabel="Close copy selector" onClose={() => skillPicker.set(null)} />
      {skill.copies.map((copy) => {
        const deletable = () => skillRowSupportsDelete(copy, mutationOperations());
        const location = skillCopyLocation(copy);
        return (
          <View class="flex-row items-center gap-2 rounded-(--radius-control) bg-sheet-surface px-3 py-2">
            <View class="min-w-0 flex-1 gap-0.5">
              <AgentMarks agents={copy.agents as ManagedSkillAgent[]} size={16} />
              <Text class="text-sub line-clamp-2">{location.label}</Text>
              <View selectable><Text class="font-mono text-code text-muted-foreground line-clamp-1">{location.path}</Text></View>
              <If when={!deletable()}><Text class="text-caption text-muted-foreground">{copy.capability.reason || "This copy cannot be deleted from here."}</Text></If>
            </View>
            <If when={deletable()}><Button
                testID={`pick-delete-${copy.id}`}
                accessibilityLabel={`Delete ${copy.name} from ${copy.location}`}
                disabled={Boolean(preparing())}
                onPress={() => { skillPicker.set(null); void deleteSkillCopy(copy); }}
                class="size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-50"
              >
                <If when={preparing() === `delete:${copy.id}`} else={<Icon name="trash" size={20} tone="ink" />}><Spinner size={16} /></If>
              </Button></If>
          </View>
        );
      })}
    </View>
  );
  const pluginPickerView = (plugin: LogicalPlugin) => (
    <View testID="plugin-copy-picker" class="gap-2">
      <SheetHeader title="Choose copy" detail={plugin.displayName} closeLabel="Close Plugin copy selector" onClose={() => pluginPicker.set(null)} />
      {plugin.copies.map((copy) => (
        <PluginCopyCard copy={copy} onUninstall={(picked) => { pluginPicker.set(null); void uninstallPluginCopy(picked); }} />
      ))}
    </View>
  );
  // Keyed by what they show, so a poll or a file picked doesn't build them again.
  const panel = computed(
    () => (inspected() && section() === "skills" ? `skill:${inspected()!.id}` : openPlugin() && section() === "plugins" ? `plugin:${openPlugin()!.key}` : ""),
    { equals: (a, b) => a === b },
  );

  return (
    <Page
      display
      title={agentExtensionsScopeLabel(props.scope)}
      hero={
        <View testID="agent-hero" class="gap-3 px-1 pb-2 pt-3">
          <LeadHero lead={props.scope === "shared" ? tile("swap-horizontal") : { kind: "logo", node: <AgentIcon kind={props.scope === "claude-code" ? "claude" : props.scope} size={40} slot={40} /> }} />
          <Text class="text-display font-bold tracking-display" role="header" style={{ numberOfLines: 2 }}>{agentExtensionsScopeLabel(props.scope)}</Text>
        </View>
      }
      testID="agent-page"
      refreshing={loadingNow() && Boolean(skillsRequestData(skillsState()))}
      onRefresh={refresh}
      aside={
        <View testID="details-panel" class="w-[400px] flex-col border-l border-border bg-card" style={{ display: wide() && panel() ? "flex" : "none" }}>
          {((key) => untracked(() => (<If when={key}><Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-4 pb-8 pt-4">{key.startsWith("skill:") ? inspector() : pluginDetails()}</Scroll></If>)))(wide() ? panel() : "")}
        </View>
      }
    >
      <If when={daemon() && projectCwd()}><View testID="project-scope" class="mt-2 flex-row items-center gap-1.5 self-start rounded-full bg-secondary px-3 py-1" accessible accessibilityLabel={`Project ${projectName()}`}>
          <Icon name="folder" size={14} />
          <Text class="text-caption line-clamp-1">{projectName()}</Text>
        </View></If>
      <If when={hasPlugins}><View class="mt-2">
          <Segmented
            testID="agent-section"
            accessibilityLabel="Plugins and Skills"
            options={[{ key: "plugins", label: "Plugins" }, { key: "skills", label: "Skills" }] as const}
            value={section()}
            onChange={(key) => chosen.set(key)}
          />
        </View></If>
      <If when={notice()}><View testID="mutation-notice">
          <Notice live tone={notice()!.kind === "error" ? "failed" : "success"} title={notice()!.message} action="Dismiss" onAction={() => notice.set(null)} />
        </View></If>
      <View testID="agent-sections" panActivation={swipes ? { activeOffsetX: 24, failOffsetY: 14 } : undefined} onPanEnd={swipes ? (event) => !event.cancelled && swipe(event.translation.x) : undefined}>
      <If when={section() === "skills"} else={<PluginsList
          rows={shownPlugins()}
          total={plugins().length}
          query={pluginQuery()}
          filters={pluginFilters()}
          onQuery={(value) => pluginQuery.set(value)}
          onFilters={() => filtersOpen.set("plugins")}
          onClearFilters={() => { pluginQuery.set(""); pluginFilters.set(NO_PLUGIN_FILTERS); }}
          onRemoveFilter={(next) => pluginFilters.set(next)}
          onOpen={(plugin) => openPluginKey.set(plugin.key)}
          onUninstall={chooseUninstall}
          nav={props.nav}
        />}><SkillsList
          rows={shownSkills()}
          total={skills().length}
          pluginOwned={hasPlugins ? pluginOwned() : 0}
          query={skillQuery()}
          filters={skillFilters()}
          onQuery={(value) => skillQuery.set(value)}
          onFilters={() => filtersOpen.set("skills")}
          onClearFilters={() => { skillQuery.set(""); skillFilters.set(NO_SKILL_FILTERS); }}
          onRemoveFilter={(next) => skillFilters.set(next)}
          onShowPlugins={() => chosen.set("plugins")}
          onOpen={(skill) => void inspectSkill(skill.primaryCopy)}
          onDelete={chooseDelete}
          nav={props.nav}
        /></If>
      </View>

      <Sheet open={filtersOpen() === "skills"} onOpenChange={({ open }) => !open && filtersOpen.set(null)} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
        <SkillFilterSheet agents={filterAgents()} filters={skillFilters()} onChange={(next) => skillFilters.set(next)} onClose={() => filtersOpen.set(null)} />
      </Sheet>
      <Sheet open={filtersOpen() === "plugins"} onOpenChange={({ open }) => !open && filtersOpen.set(null)} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
        <PluginFilterSheet agents={MANAGED_SKILL_AGENTS.filter((agent) => plugins().some((plugin) => plugin.agents.includes(agent)))} filters={pluginFilters()} onChange={(next) => pluginFilters.set(next)} onClose={() => filtersOpen.set(null)} />
      </Sheet>

      <Sheet open={skillPicker() !== null} onOpenChange={({ open }) => !open && skillPicker.set(null)} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
        <View>{((skill) => untracked(() => (skill ? skillPickerView(skill) : null)))(skillPicker())}</View>
      </Sheet>
      <Sheet open={pluginPicker() !== null} onOpenChange={({ open }) => !open && pluginPicker.set(null)} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
        <View>{((plugin) => untracked(() => (plugin ? pluginPickerView(plugin) : null)))(pluginPicker())}</View>
      </Sheet>

      <Sheet open={!wide() && panel().startsWith("skill:")} onOpenChange={({ open }) => !open && closeInspector()} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
        {/* The sheet takes its content once; the view inside follows the copy shown. */}
        <View class="shrink">{((key) => untracked(() => (<If when={key.startsWith("skill:")}><Scroll keyboardShouldPersistTaps="handled" class="shrink">{inspector()}</Scroll></If>)))(wide() ? "" : panel())}</View>
      </Sheet>
      <Sheet open={!wide() && panel().startsWith("plugin:")} onOpenChange={({ open }) => !open && openPluginKey.set(null)} closeOnSwipe popupClass={SHEET} backdropClass={BACKDROP}>
        <View class="shrink">{((key) => untracked(() => (<If when={key.startsWith("plugin:")}><Scroll keyboardShouldPersistTaps="handled" class="shrink">{pluginDetails()}</Scroll></If>)))(wide() ? "" : panel())}</View>
      </Sheet>
    </Page>
  );
}

/** Search and Filter above a list. */
function Toolbar(props: { placeholder: string; label: string; query: string; onQuery: (value: string) => void; filterLabel: string; filtered: boolean; onFilters: () => void; testID: string }) {
  return (
    <View class="mt-4 flex-row items-center gap-2">
      <View class="flex-1">
        <SearchField testID={`${props.testID}-search`} accessibilityLabel={props.label} placeholder={props.placeholder} value={props.query} onChange={props.onQuery} />
      </View>
      <Button testID={`${props.testID}-filter`} accessibilityLabel={props.filterLabel} onPress={props.onFilters} class={`size-11 items-center justify-center rounded-[14px] active:bg-accent ${props.filtered ? "bg-primary" : "bg-card"}`}>
        <Icon name="options" size={18} tone={props.filtered ? "canvas" : "soft"} />
      </Button>
    </View>
  );
}

/** An active filter, removable on its own. */
function Chip(props: { label: string; removeLabel: string; onRemove: () => void }) {
  return (
    <Button accessibilityLabel={props.removeLabel} onPress={props.onRemove} class="flex-row items-center gap-1 rounded-full bg-secondary px-3 py-1 active:opacity-70">
      <Text class="text-caption font-semibold">{props.label}</Text>
      <Icon name="close" size={12} />
    </Button>
  );
}

/** One logical item: its name and line, the agents it serves, and its one action (remove, or why not). */
function ItemRow(props: { testID: string; name: string; summary: string; agents: readonly ManagedSkillAgent[]; openLabel: string; action: { label: string; icon: IconName; destructive: boolean; busy: boolean; disabled: boolean; onPress: () => void }; onOpen: () => void }) {
  return (
    <View class="flex-row items-center">
      <Button testID={props.testID} accessibilityLabel={props.openLabel} onPress={props.onOpen} class="min-w-0 flex-1 gap-1 py-3 pl-4 pr-2 active:bg-accent">
        <Text class="text-body font-semibold line-clamp-1">{props.name}</Text>
        <Text class="text-sub text-muted-foreground line-clamp-2">{props.summary}</Text>
        <AgentMarks agents={props.agents} size={15} />
      </Button>
      <Button testID={`${props.testID}-action`} accessibilityLabel={props.action.label} disabled={props.action.disabled} onPress={props.action.onPress} class="mr-1 size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-50">
        <If when={props.action.busy} else={<Icon name={props.action.icon} size={20} tone={props.action.destructive ? "ink" : "faint"} />}><Spinner size={16} /></If>
      </Button>
      <View class="absolute bottom-0 left-4 right-0 h-px bg-border" />
    </View>
  );
}

function Card(props: { children?: JSX.Element }) {
  return (
    <View class="mt-3 overflow-hidden rounded-(--radius-group) bg-card">
      <View class="-mb-px">{props.children}</View>
    </View>
  );
}

function SkillsList(props: {
  rows: LogicalSkill[];
  total: number;
  pluginOwned: number;
  query: string;
  filters: SkillFilters;
  onQuery: (value: string) => void;
  onFilters: () => void;
  onClearFilters: () => void;
  onRemoveFilter: (next: SkillFilters) => void;
  onShowPlugins: () => void;
  onOpen: (skill: LogicalSkill) => void;
  onDelete: (skill: LogicalSkill) => void;
  nav: PageNav;
}) {
  const state = () => skillsState();
  const inventory = () => skillsRequestData(state());
  const filtered = () => props.filters.agents.length > 0 || props.filters.status !== "all" || props.filters.scope !== "all";
  const offline = () => daemonState() !== "connected";
  const body = () => {
    if (!daemon()) return <EmptyState icon="server" title="No current server" detail="Choose one in Settings." action="Open Settings" onAction={() => props.nav.push({ name: "settings" })} />;
    if (!inventory() && daemonState() === "connecting") return <EmptyState busy title="Connecting to server" />;
    if (!inventory() && state().status === "error") {
      return (
        <View>
          <EmptyState testID="skills-failed" icon={offline() ? "cloud-offline" : "warning"} title={offline() ? "Server disconnected" : "Skills unavailable"} detail={state().error} action="Try again" onAction={() => void refreshSkills()} />
          <If when={offline()}><View class="items-center"><PillButton title="Open Settings" onPress={() => props.nav.push({ name: "settings" })} /></View></If>
        </View>
      );
    }
    if (!inventory()) return <EmptyState busy title="Loading local Skills" />;
    if (!props.total && !props.pluginOwned) return <EmptyState icon="skills" title="No local Skills" action="Refresh" onAction={() => void refreshSkills()} />;
    if (!props.total && !filtered() && !props.query && props.pluginOwned > 0) {
      return <EmptyState icon="plugins" title="All Skills come with plugins" action="Show plugins" onAction={props.onShowPlugins} />;
    }
    if (!props.rows.length) return <EmptyState icon="search" title="No matches" action="Clear filters" onAction={props.onClearFilters} />;
    return (
      <Card>
        {props.rows.map((skill) => {
          const canDelete = skill.copies.some((copy) => skillRowSupportsDelete(copy, mutationOperations()));
          const readonlyReason = skill.copies.find((copy) => !copy.capability.canDelete && copy.capability.reason)?.capability.reason;
          return (
            <ItemRow
              testID={`skill-${skill.name}`}
              name={skill.name}
              summary={skill.description || readonlyReason || "Local Skill"}
              agents={skill.agents}
              openLabel={`Open ${skill.name}`}
              onOpen={() => props.onOpen(skill)}
              action={{
                label: canDelete ? `Delete ${skill.name}` : `Open why ${skill.name} is protected`,
                icon: canDelete ? "trash" : "lock",
                destructive: canDelete,
                busy: skill.copies.some((copy) => preparing() === `delete:${copy.id}`),
                disabled: canDelete && Boolean(preparing()),
                onPress: () => (canDelete ? props.onDelete(skill) : props.onOpen(skill)),
              }}
            />
          );
        })}
      </Card>
    );
  };
  return (
    <View>
      <If when={inventory()}><Toolbar testID="skills" placeholder="Search Skills" label="Search local Skills" query={props.query} onQuery={props.onQuery} filterLabel="Filter Skills" filtered={filtered()} onFilters={props.onFilters} /></If>
      <If when={filtered()}><View class="mt-2 flex-row flex-wrap gap-1.5">
          {props.filters.agents.map((agent) => (
            <Chip label={skillAgentLabel(agent)} removeLabel={`Remove ${skillAgentLabel(agent)} filter`} onRemove={() => props.onRemoveFilter({ ...props.filters, agents: props.filters.agents.filter((item) => item !== agent) })} />
          ))}
          <If when={props.filters.status !== "all"}><Chip label={STATUS_LABEL[props.filters.status]} removeLabel={`Remove ${STATUS_LABEL[props.filters.status]} filter`} onRemove={() => props.onRemoveFilter({ ...props.filters, status: "all" })} /></If>
          <If when={props.filters.scope !== "all"}><Chip label={SCOPE_LABEL[props.filters.scope]} removeLabel={`Remove ${SCOPE_LABEL[props.filters.scope]} filter`} onRemove={() => props.onRemoveFilter({ ...props.filters, scope: "all" })} /></If>
        </View></If>
      <OwnershipNotice count={props.pluginOwned} onView={props.onShowPlugins} />
      {body()}
    </View>
  );
}

/** Where the Skills a plugin brings are: inside it, or, while ownership is unknown, here and protected. */
function OwnershipNotice(props: { count: number; onView: () => void }) {
  const state = () => pluginsState();
  return (
    <View>
      <If when={state().status === "error"} else={<If when={props.count > 0}><Notice testID="ownership-notice" tone="info" icon="puzzle" title={`${props.count} more ${props.count === 1 ? "Skill comes" : "Skills come"} with plugins`} action="View" onAction={props.onView} /></If>}><Notice testID="ownership-failed" title="Plugin ownership unavailable" detail="Skills that come with plugins may appear here. They stay protected." action="Retry" onAction={() => void refreshPlugins()} /></If>
    </View>
  );
}

function PluginsList(props: {
  rows: LogicalPlugin[];
  total: number;
  query: string;
  filters: PluginFilters;
  onQuery: (value: string) => void;
  onFilters: () => void;
  onClearFilters: () => void;
  onRemoveFilter: (next: PluginFilters) => void;
  onOpen: (plugin: LogicalPlugin) => void;
  onUninstall: (plugin: LogicalPlugin) => void;
  nav: PageNav;
}) {
  const state = () => pluginsState();
  const inventory = () => skillsRequestData(state());
  const filtered = () => props.filters.agents.length > 0 || props.filters.capability !== "all";
  const body = () => {
    if (!daemon()) return <EmptyState icon="server" title="No current server" detail="Choose one in Settings." action="Open Settings" onAction={() => props.nav.push({ name: "settings" })} />;
    if (!inventory() && state().status === "error") return <EmptyState testID="plugins-failed" icon="warning" title="Plugins unavailable" detail={state().error} action="Try again" onAction={() => void refreshPlugins()} />;
    if (!inventory()) return <EmptyState busy title="Loading installed Plugins" />;
    if (!props.total) return <EmptyState icon="plugins" title="No plugins installed" action="Refresh" onAction={() => void refreshPlugins()} />;
    if (!props.rows.length) return <EmptyState icon="search" title="No matches" action="Clear search" onAction={props.onClearFilters} />;
    return (
      <Card>
        {props.rows.map((plugin) => {
          const canUninstall = plugin.copies.some((copy) => copy.capability.canUninstall);
          const readonlyReason = plugin.copies.find((copy) => !copy.capability.canUninstall && copy.capability.reason)?.capability.reason;
          return (
            <ItemRow
              testID={`plugin-${plugin.name}`}
              name={plugin.displayName}
              summary={plugin.description || readonlyReason || "Local Plugin"}
              agents={plugin.agents.filter(isManaged)}
              openLabel={`Open ${plugin.displayName} Plugin details`}
              onOpen={() => props.onOpen(plugin)}
              action={{
                label: canUninstall ? `Uninstall ${plugin.displayName}` : `Open why ${plugin.displayName} is protected`,
                icon: canUninstall ? "trash" : "lock",
                destructive: canUninstall,
                busy: plugin.copies.some((copy) => preparing() === `uninstall:${copy.copyId}`),
                disabled: canUninstall && Boolean(preparing()),
                onPress: () => (canUninstall ? props.onUninstall(plugin) : props.onOpen(plugin)),
              }}
            />
          );
        })}
      </Card>
    );
  };
  return (
    <View>
      <If when={inventory()}><Toolbar testID="plugins" placeholder="Search Plugins" label="Search installed Plugins" query={props.query} onQuery={props.onQuery} filterLabel="Filter Plugins" filtered={filtered()} onFilters={props.onFilters} /></If>
      <If when={filtered()}><View class="mt-2 flex-row flex-wrap gap-1.5">
          {props.filters.agents.map((agent) => (
            <Chip label={skillAgentLabel(agent)} removeLabel={`Remove ${skillAgentLabel(agent)} Plugin filter`} onRemove={() => props.onRemoveFilter({ ...props.filters, agents: props.filters.agents.filter((item) => item !== agent) })} />
          ))}
          <If when={props.filters.capability !== "all"}><Chip label={CAPABILITY_LABEL[props.filters.capability]} removeLabel="Remove Plugin capability filter" onRemove={() => props.onRemoveFilter({ ...props.filters, capability: "all" })} /></If>
        </View></If>
      {body()}
    </View>
  );
}

const STATUS_LABEL: Record<SkillStatusFilter, string> = { all: "All", enabled: "Enabled", disabled: "Disabled" };
const SCOPE_LABEL: Record<SkillScopeFilter, string> = { all: "All", global: "Global", project: "Project" };
const CAPABILITY_LABEL: Record<PluginCapabilityFilter, string> = { all: "All", uninstallable: "Uninstallable", readonly: "Read-only" };

/** One exclusive choice inside a filter sheet. */
function Choice<K extends string>(props: { title: string; options: readonly K[]; labels: Record<K, string>; value: K; onChange: (value: K) => void; testID: string }) {
  return (
    <View class="mt-4 gap-2">
      <Text class="text-sub font-semibold text-muted-foreground" role="header">{props.title}</Text>
      <View class="flex-row flex-wrap gap-2">
        {props.options.map((option) => (
          <Button
            testID={`${props.testID}-${option}`}
            role="radio"
            selected={props.value === option}
            onPress={() => props.onChange(option)}
            class={`rounded-full border px-3 py-1.5 active:opacity-70 ${props.value === option ? "border-foreground bg-secondary" : "border-border"}`}
          >
            <Text class="text-sub">{props.labels[option]}</Text>
          </Button>
        ))}
      </View>
    </View>
  );
}

function AgentChoice(props: { agents: readonly ManagedSkillAgent[]; value: readonly ManagedSkillAgent[]; onChange: (agents: ManagedSkillAgent[]) => void; title: string }) {
  return (
    <View class="mt-4 flex-col gap-2" style={{ display: props.agents.length < 2 ? "none" : "flex" }}>
      <Text class="text-sub font-semibold text-muted-foreground" role="header">{props.title}</Text>
      <View class="flex-row flex-wrap gap-2">
        {props.agents.map((agent) => {
          const on = props.value.includes(agent);
          return (
            <Button
              testID={`filter-agent-${agent}`}
              role="checkbox"
              selected={on}
              onPress={() => props.onChange(on ? props.value.filter((item) => item !== agent) : [...props.value, agent])}
              class={`rounded-full border px-3 py-1.5 active:opacity-70 ${on ? "border-foreground bg-secondary" : "border-border"}`}
            >
              <Text class="text-sub">{skillAgentLabel(agent)}</Text>
            </Button>
          );
        })}
      </View>
    </View>
  );
}

function SheetButtons(props: { onReset: () => void; onDone: () => void }) {
  return (
    <View class="mt-6 flex-row justify-end gap-2">
      <PillButton title="Reset" onPress={props.onReset} />
      <PillButton testID="filter-done" title="Done" filled onPress={props.onDone} />
    </View>
  );
}

function SkillFilterSheet(props: { agents: ManagedSkillAgent[]; filters: SkillFilters; onChange: (next: SkillFilters) => void; onClose: () => void }) {
  return (
    <View testID="skill-filters">
      <Text class="text-xl font-semibold" role="header">Filter Skills</Text>
      <AgentChoice title="Agents" agents={props.agents} value={props.filters.agents} onChange={(agents) => props.onChange({ ...props.filters, agents })} />
      <Choice testID="filter-status" title="Status" options={["all", "enabled", "disabled"] as const} labels={STATUS_LABEL} value={props.filters.status} onChange={(status) => props.onChange({ ...props.filters, status })} />
      <Choice testID="filter-scope" title="Scope" options={["all", "global", "project"] as const} labels={SCOPE_LABEL} value={props.filters.scope} onChange={(scope) => props.onChange({ ...props.filters, scope })} />
      <SheetButtons onReset={() => props.onChange(NO_SKILL_FILTERS)} onDone={props.onClose} />
    </View>
  );
}

function PluginFilterSheet(props: { agents: ManagedSkillAgent[]; filters: PluginFilters; onChange: (next: PluginFilters) => void; onClose: () => void }) {
  return (
    <View testID="plugin-filters">
      <Text class="text-xl font-semibold" role="header">Filter Plugins</Text>
      <AgentChoice title="Available to" agents={props.agents} value={props.filters.agents} onChange={(agents) => props.onChange({ ...props.filters, agents })} />
      <Choice testID="filter-capability" title="Capability" options={["all", "uninstallable", "readonly"] as const} labels={CAPABILITY_LABEL} value={props.filters.capability} onChange={(capability) => props.onChange({ ...props.filters, capability })} />
      <SheetButtons onReset={() => props.onChange(NO_PLUGIN_FILTERS)} onDone={props.onClose} />
    </View>
  );
}

/** Android's Back while the page lives; a browser has its own back (NSH's web runtime has no `BackHandler.on`). */
function onAndroidBack(listener: () => boolean): void {
  if (platform.os === "web") return;
  cleanup(BackHandler.on("press", listener));
}
