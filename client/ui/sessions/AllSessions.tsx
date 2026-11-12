import { cleanup, computed, createMotion, effect, List, platform, signal, Storage, Text, untracked, View } from "nsh";
import { Button, type NavStackState } from "nsh/base";
import { DisplayTitle, PillButton, SectionLabel, Tile } from "../kit/kit";
import { BackHandler } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { makeSessionKey } from "../../core/sessions/sessionKeys";
import { sessionRowModel } from "../../core/sessions/listRow";
import { workLinkOf } from "./workLinks";
import { openPage } from "../shell/nav";
import { newSessionOpen } from "../screens/NewSession";
import { haptic } from "../shell/haptics";
import { buildSessionListSections, collapsedSectionsToStore, toggleCollapsedSection, type SessionListSection } from "../../core/sessions/sessionSections";
import { sessionEmptyCat, sessionEmptyState } from "../../core/sessions/sessionEmptyState";
import { PetSprite } from "../pets/PetSprite";
import { countSessionSelection, EMPTY_SESSION_SELECTION, pruneSessionSelection, selectionCountLabel, toggleSessionSelection, type SessionSelection } from "../../core/sessions/sessionSelection";
import {
  SessionTerminationBatch,
  createSessionTerminationEntries,
  sessionTerminationConfirmMessage,
  sessionTerminationSummaryMessage,
} from "../../core/sessions/sessionBulkTerminate";
import { getCollapsedSessionSections, getWorkerAliases, markWorkerOpened, setCollapsedSessionSections, type StoredWorkerAliases } from "../../core/sessions/deviceState";
import { groupWorkersByDirectory } from "../../core/sessions/workerDirectory";
import { newRowKeys } from "../../core/sessions/newRows";
import type { Worker } from "../../core/sessions/workers";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon, type ColorToken, type IconName } from "../icons/Icon";
import { connection, current, currentTransport, resumeConnection, workers, workersHydrated } from "../session";
import type { Route } from "../shell/routes";
import { showAlert } from "../shell/alert";

const aliases = signal<StoredWorkerAliases>({});
const collapsed = signal<ReadonlySet<string>>(new Set());
const selection = signal<SessionSelection>(EMPTY_SESSION_SELECTION);
const terminating = signal(false);
const notice = signal("");
/** Rows new since the list last changed: they fade in (`newRowKeys`). Derived with the list, so a new row never reads it stale. */
let knownRows: ReadonlySet<string> | null = null;
const freshRows = computed<ReadonlySet<string>>(() => {
  const keys = workers().map((w) => w.key);
  const fresh = newRowKeys(knownRows, keys);
  knownRows = new Set(keys);
  return fresh;
});

/** Nicknames are saved on this device; the Session screen's Rename changes them. */
export async function reloadAliases(): Promise<void> {
  aliases.set(await getWorkerAliases(Storage).catch(() => ({})));
}
void reloadAliases();
void getCollapsedSessionSections(Storage).then((keys) => collapsed.set(new Set(keys))).catch(() => {});

const selecting = computed(() => countSessionSelection(selection()) > 0);

/** A row knows where it sits in its folder's surface group: the first rounds the top, the last the bottom and has no hair. */
type Row = { key: string; kind: "header"; section: SessionListSection } | { key: string; kind: "row"; worker: Worker; first: boolean; last: boolean };

const rows = computed<Row[]>(() => {
  const sections = buildSessionListSections(groupWorkersByDirectory(workers()), collapsed(), selecting());
  const out: Row[] = [];
  for (const section of sections) {
    if (sections.length > 1) out.push({ key: `h:${section.key}`, kind: "header", section });
    section.data.forEach((worker, i) => out.push({ key: worker.key, kind: "row", worker, first: i === 0, last: i === section.data.length - 1 }));
  }
  return out;
});

function toggleSection(key: string): void {
  const next = toggleCollapsedSection(collapsed(), key);
  collapsed.set(next);
  void setCollapsedSessionSections(Storage, collapsedSectionsToStore(next)).catch(() => {});
}

function toggleSelected(worker: Worker): void {
  if (connection() !== "connected") return;
  selection.set(toggleSessionSelection(selection(), worker.key));
}

async function terminateSelected(): Promise<void> {
  const transport = currentTransport();
  const server = current();
  const keys = [...selection()];
  if (!transport || !server || keys.length === 0 || terminating()) return;
  haptic.notification("warning");
  // Named by what it ends; the action keeps the normal role (red is for what waits for you).
  const only = keys.length === 1 ? workers().find((w) => w.key === keys[0]) : undefined;
  const { action } = await showAlert({
    title: only ? `Terminate ${sessionRowModel(only, { alias: untracked(aliases)[only.id] }).title}?` : `Terminate ${keys.length} sessions?`,
    description: sessionTerminationConfirmMessage(keys.length),
    actions: [{ id: "cancel", label: "Cancel", role: "cancel" }, { id: "terminate", label: "Terminate" }],
  });
  if (action !== "terminate") return;
  const byKey = new Map(workers().map((w) => [w.key, w] as const));
  const targets = keys.flatMap((key) => {
    const worker = byKey.get(key);
    return worker ? [{ sessionKey: key, serverId: server.id, workerId: worker.id }] : [];
  });
  terminating.set(true);
  const batch = new SessionTerminationBatch({
    transport,
    entries: createSessionTerminationEntries(targets),
    onSettled: (summary) => {
      if (summary.running) return;
      terminating.set(false);
      const failed = new Set(summary.failedEntries.map((e) => e.sessionKey));
      selection.set(failed.size ? new Set([...failed]) : EMPTY_SESSION_SELECTION);
      const titles = summary.failedEntries.map((e) => (byKey.get(e.sessionKey) ? sessionRowModel(byKey.get(e.sessionKey)!, { alias: untracked(aliases)[e.workerId] }).title : ""));
      notice.set(sessionTerminationSummaryMessage(summary, titles));
      if (!failed.size) haptic.notification("success");
    },
  });
  batch.start();
}

const STATUS_MARK: Record<string, { icon: IconName; color: ColorToken } | undefined> = {
  done: { icon: "check-circle-fill", color: "muted-foreground" },
  failed: { icon: "close-circle-fill", color: "destructive" },
  blocked: { icon: "pause-circle", color: "warning" },
};

/** A folder's section label (SPEC §15 Grouped list): a folder icon, the name, the count at the right; a tap folds it. */
function SectionHeader(props: { section: () => SessionListSection }) {
  const s = props.section;
  return (
    <SectionLabel
      testID="section"
      icon="folder"
      title={s().title}
      count={s().count}
      folded={s().collapsed}
      onPress={() => !selecting() && toggleSection(s().key)}
      trailing={s().mark === "needs" ? <Icon name="hand" size={14} color="seal" /> : s().mark === "failed" ? <Icon name="close-circle-fill" size={14} color="destructive" /> : s().mark === "running" ? <Icon name="time" size={14} /> : undefined}
    />
  );
}

/** A Session as a tray row (SPEC §11): 68 pt, a 28 pt tile, the title over its line, the end; in its folder's surface group. */
function SessionRow(props: { worker: () => Worker; first: () => boolean; last: () => boolean; open: (worker: Worker) => void }) {
  const model = () => {
    const link = workLinkOf(props.worker());
    return sessionRowModel(props.worker(), { alias: aliases()[props.worker().id], workTitle: link?.workTitle, calendarLabel: link?.calendarLabel });
  };
  const selected = () => selection().has(props.worker().key);
  // Only a reachable computer can end its Sessions: offline rows can't be picked.
  const online = () => connection() === "connected";
  // A row new to the list fades in once; rows that scroll into view never do.
  const motion = untracked(() => freshRows().has(props.worker().key)) ? createMotion({ enter: "fadeIn", duration: 220 }) : null;
  const toneClass = () => (model().tone === "danger" ? "text-destructive" : model().tone === "needs" ? "text-seal" : model().tone === "accent" ? "text-foreground" : "text-muted-foreground");
  return (
    <View testID="session-row" delayLongPress={400} style={motion?.style}
      accessibilityLabel={model().accessibilityLabel} role={selecting() ? "checkbox" : "button"}
      accessibilityState={selecting() ? { checked: selected(), disabled: !online() } : undefined}
      accessibilityHint={selecting() ? "Double tap to toggle selection" : "Opens the terminal session"}
      onPress={() => (selecting() ? online() && toggleSelected(props.worker()) : props.open(props.worker()))}
      onLongPress={() => {
        if (selecting() || !online()) return;
        haptic.impact("medium");
        toggleSelected(props.worker());
      }}
      onContextMenu={() => {
        // A right-click starts selecting, as a long press does on touch (parity #28, SPEC §14.4).
        if (!selecting() && online()) toggleSelected(props.worker());
      }}
      class={`mx-4 min-h-[68px] flex-row items-center gap-3.5 bg-card px-4 py-3 ${props.first() ? "rounded-t-(--radius-group)" : ""} ${props.last() ? "rounded-b-(--radius-group)" : ""} ${selecting() && !online() ? "opacity-50" : "active:bg-accent"}`}>
      {selecting() ? <Icon name={selected() ? "check-circle-fill" : "circle"} size={22} color={selected() ? "primary" : "muted-foreground"} /> : null}
      <View class="items-center justify-center">
        <Tile size={28}><AgentIcon kind={model().kind} flavor={model().flavor} size={18} /></Tile>
        {model().fromCalendar ? <View testID="row-calendar" class="absolute -bottom-0.5 -right-0.5 rounded-full bg-background p-0.5"><Icon name="calendar" size={10} color="primary" /></View>
          : model().delegated ? <View class="absolute -bottom-0.5 -right-0.5 rounded-full bg-background p-0.5"><Icon name="git-network" size={10} color="primary" /></View> : null}
      </View>
      <View class="min-w-0 flex-1">
        <Text class="text-body font-semibold line-clamp-1">{model().title}</Text>
        <Text class={`text-sub line-clamp-1 ${toneClass()}`}>{`${model().previewPrefix ?? ""}${model().preview}`}</Text>
      </View>
      <View class="items-end gap-1">
        {model().time !== "live" ? <Text class="text-sub text-muted-foreground">{model().time}</Text> : null}
        {model().needsYou ? <Icon name="hand" size={16} color="seal" />
          : model().status === "running" ? <View class="h-2 w-2 rounded-full bg-primary" />
          : STATUS_MARK[model().status] ? <Icon name={STATUS_MARK[model().status]!.icon} size={16} color={STATUS_MARK[model().status]!.color} /> : null}
      </View>
      {props.last() ? null : <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: selecting() ? 94 : 58 }} />}
    </View>
  );
}

function Empty(props: { onPair: () => void; onNew: () => void }) {
  const state = () => sessionEmptyState(Boolean(current()), connection() === "waiting" ? "offline" : connection() === "idle" ? "connecting" : connection());
  const loading = () => connection() === "connected" && !workersHydrated();
  const cat = () => sessionEmptyCat(state().action, loading() || Boolean(state().busy));
  return (
    <View class="flex-1 items-center justify-center gap-3 p-8">
      {/* The chosen pet, in the state's clip (waking while it loads); a filter that matches nothing keeps its icon. */}
      {cat() ? <PetSprite testID="sessions-empty-cat" state={() => cat()!} size={104} />
        : <Icon name={(loading() ? "server" : state().icon) as IconName} size={40} />}
      <Text testID="sessions-empty" class="text-center text-title font-bold">{loading() ? "Loading sessions" : state().title}</Text>
      {!loading() && state().action ? (
        <Button testID="sessions-empty-action" onPress={() => (state().action === "pair" ? props.onPair() : state().action === "retry" ? resumeConnection() : props.onNew())}
          class="h-[48px] items-center justify-center rounded-(--radius-control) bg-primary px-5 active:opacity-80">
          <Text class="text-body font-semibold text-primary-foreground">{state().label}</Text>
        </Button>
      ) : null}
    </View>
  );
}

/**
 * The current computer's Sessions, grouped by directory (a caption per
 * directory once there are two; a tap folds it). A tap opens a Session; a
 * long press starts selecting, and the bar terminates what is selected.
 */
export function AllSessions(props: { nav: NavStackState<Route> }) {
  const onTop = () => props.nav.routes().at(-1)?.name === "allSessions";
  const open = (worker: Worker) => {
    void markWorkerOpened(Storage, worker.id).catch(() => {});
    props.nav.push({ name: "session", workerId: worker.id, hint: { name: worker.name, cwd: worker.cwd, command: worker.command, startedAt: worker.started_at } });
  };
  const onPair = () => openPage({ name: "computer" }, { name: "pair" });
  const onNew = () => newSessionOpen.set(true);
  // Android's Back ends selecting first (parity #26), as the Expo list did.
  onAndroidBack(() => {
    if (!untracked(selecting) || !untracked(onTop)) return false;
    selection.set(EMPTY_SESSION_SELECTION);
    return true;
  });
  // Leaving the page ends selecting.
  effect(() => {
    if (!onTop() && untracked(selecting)) selection.set(EMPTY_SESSION_SELECTION);
  });
  // A Session that left the list cannot stay selected.
  effect(() => {
    const next = pruneSessionSelection(selection(), workers().map((w) => w.key));
    if (next !== untracked(selection)) selection.set(next);
  });
  // The nav bar names the page once the list has scrolled past its display title.
  const titleAway = signal(false);
  return (
    <NavPage title={titleAway() && !selecting() ? "All Sessions" : ""}>
      <View class="flex-1 bg-background">
        {selecting() ? (
          <View class="min-h-[52px] flex-row items-center gap-2 px-2">
            <Button testID="selection-cancel" accessibilityLabel="Cancel" onPress={() => selection.set(EMPTY_SESSION_SELECTION)} class="size-11 items-center justify-center rounded-full active:bg-accent">
              <Icon name="close" size={22} color="foreground" />
            </Button>
            <Text class="flex-1 text-title font-bold" role="header">{selectionCountLabel(countSessionSelection(selection()))}</Text>
          </View>
        ) : null}
        {notice() ? <Text testID="sessions-notice" class="px-4 py-2 text-sub text-destructive" onPress={() => notice.set("")}>{notice()}</Text> : null}
        <List keyboardShouldPersistTaps="handled" testID="sessions" class="flex-1" each={rows()} keyExtractor={(row) => row.key} contentClass="pb-28"
          header={<View class="px-4"><DisplayTitle title="All Sessions" /></View>}
          onViewableItemsChange={({ viewableItems }) => titleAway.set(viewableItems.length > 0 && !viewableItems.some((t) => t.index === 0))}
          empty={<Empty onPair={onPair} onNew={onNew} />}>
          {(row) => {
            // Cells are recycled: what a cell shows follows the row it holds now. A Session's update makes new
            // rows, so the cell is rebuilt only when it turns from a heading into a Session or back: a tap during
            // an update lands on the row, not on one being replaced. Each side keeps its last value while it goes.
            const header = computed(() => row().kind === "header");
            const section = computed<SessionListSection>((last) => (row().kind === "header" ? (row() as Extract<Row, { kind: "header" }>).section : last!));
            const item = computed<Extract<Row, { kind: "row" }>>((last) => (row().kind === "header" ? last! : (row() as Extract<Row, { kind: "row" }>)));
            return (
              <View>
                {header() ? <SectionHeader section={section} /> : <SessionRow worker={() => item().worker} first={() => item().first} last={() => item().last} open={open} />}
              </View>
            );
          }}
        </List>
        {selecting() ? (
          <View class="absolute bottom-0 left-0 right-0 bg-background px-4 pb-4 pt-2">
            <PillButton
              testID="selection-terminate"
              filled
              title={terminating() ? "Terminating…" : `Terminate ${countSessionSelection(selection())} ${countSessionSelection(selection()) === 1 ? "session" : "sessions"}`}
              disabled={terminating() || connection() !== "connected"}
              onPress={() => void terminateSelected()}
            />
          </View>
        ) : null}
      </View>
    </NavPage>
  );
}

/** Builds the key the device state and the route use for a Session of the current computer. */
export function currentSessionKey(workerId: string): string | null {
  const server = current();
  return server ? makeSessionKey(server.id, workerId) : null;
}

/** Android's Back while the page lives; a browser has its own back (NSH's web runtime has no `BackHandler.on`). */
function onAndroidBack(listener: () => boolean): void {
  if (platform.os === "web") return;
  cleanup(BackHandler.on("press", listener));
}
