import { cleanup, computed, createMotion, effect, Image, List, platform, signal, Storage, Text, untracked, View } from "nsh";
import { AlertDialog, Button, type NavStackState } from "nsh/base";
import { BackHandler } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { makeSessionKey } from "../../core/sessions/sessionKeys";
import { sessionRowModel } from "../../core/sessions/listRow";
import { servicesOpen } from "../sessions/ServicesSheet";
import { workLinkOf } from "../sessions/workLinks";
import { desktop, selectedTab } from "../shell/Shell";
import { buildSessionListSections, collapsedSectionsToStore, toggleCollapsedSection, type SessionListSection } from "../../core/sessions/sessionSections";
import { sessionEmptyCat, sessionEmptyState } from "../../core/sessions/sessionEmptyState";
import { petClip } from "../pets/clips";
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
import type { SessionsRoute } from "../shell/routes";

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

type Row = { key: string; kind: "header"; section: SessionListSection } | { key: string; kind: "row"; worker: Worker };

const rows = computed<Row[]>(() => {
  const sections = buildSessionListSections(groupWorkersByDirectory(workers()), collapsed(), selecting());
  const out: Row[] = [];
  for (const section of sections) {
    if (sections.length > 1) out.push({ key: `h:${section.key}`, kind: "header", section });
    for (const worker of section.data) out.push({ key: worker.key, kind: "row", worker });
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
  const { action } = await AlertDialog.show({
    title: "Terminate sessions?",
    description: sessionTerminationConfirmMessage(keys.length),
    actions: [{ id: "cancel", label: "Cancel", role: "cancel" }, { id: "terminate", label: "Terminate", role: "destructive" }],
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
    },
  });
  batch.start();
}

const STATUS_MARK: Record<string, { icon: IconName; color: ColorToken } | undefined> = {
  done: { icon: "check-circle-fill", color: "muted-foreground" },
  failed: { icon: "close-circle-fill", color: "destructive" },
  blocked: { icon: "pause-circle", color: "warning" },
};

function SectionHeader(props: { section: () => SessionListSection }) {
  const s = props.section;
  return (
    <Button testID="section" onPress={() => !selecting() && toggleSection(s().key)} class="flex-row items-center gap-2 px-4 pb-1 pt-4">
      <Text class="flex-1 text-xs font-semibold uppercase text-muted-foreground line-clamp-1">{s().title}</Text>
      {s().collapsed ? <Text class="text-xs text-muted-foreground">{String(s().count)}</Text> : null}
      {s().mark === "needs" ? <Icon name="hand" size={14} color="seal" /> : s().mark === "failed" ? <Icon name="close-circle-fill" size={14} color="destructive" /> : s().mark === "running" ? <Icon name="time" size={14} /> : null}
      <Icon name={s().collapsed ? "chevron-right" : "chevron-down"} size={14} />
    </Button>
  );
}

function SessionRow(props: { worker: () => Worker; open: (worker: Worker) => void }) {
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
      onLongPress={() => !selecting() && online() && toggleSelected(props.worker())}
      onContextMenu={() => {
        // The desktop layout: a right-click starts selecting, as a long press does on a phone (parity #28).
        if (untracked(desktop) && !selecting() && online()) toggleSelected(props.worker());
      }}
      class={selecting() && !online() ? "flex-row items-center gap-3 px-4 py-2.5 opacity-50" : "flex-row items-center gap-3 px-4 py-2.5 active:bg-accent"}>
      {selecting() ? <Icon name={selected() ? "check-circle-fill" : "circle"} size={22} color={selected() ? "primary" : "muted-foreground"} /> : null}
      <View class="items-center justify-center">
        <AgentIcon kind={model().kind} flavor={model().flavor} size={24} />
        {model().fromCalendar ? <View testID="row-calendar" class="absolute -bottom-0.5 -right-0.5 rounded-full bg-background p-0.5"><Icon name="calendar" size={10} color="primary" /></View>
          : model().delegated ? <View class="absolute -bottom-0.5 -right-0.5 rounded-full bg-background p-0.5"><Icon name="git-network" size={10} color="primary" /></View> : null}
      </View>
      <View class="flex-1 gap-0.5">
        <Text class="text-base font-semibold line-clamp-1">{model().title}</Text>
        <Text class={`text-sm line-clamp-1 ${toneClass()}`}>{`${model().previewPrefix ?? ""}${model().preview}`}</Text>
      </View>
      <View class="items-end gap-1">
        {model().time !== "live" ? <Text class="text-xs text-muted-foreground">{model().time}</Text> : null}
        {model().needsYou ? <Icon name="hand" size={16} color="seal" />
          : model().status === "running" ? <View class="h-2 w-2 rounded-full bg-primary" />
          : STATUS_MARK[model().status] ? <Icon name={STATUS_MARK[model().status]!.icon} size={16} color={STATUS_MARK[model().status]!.color} /> : null}
      </View>
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
      {cat() ? <Image testID="sessions-empty-cat" src={petClip(cat()!)} accessibilityElementsHidden style={{ width: 104, height: 104 }} />
        : <Icon name={(loading() ? "server" : state().icon) as IconName} size={40} />}
      <Text testID="sessions-empty" class="text-center text-lg font-semibold">{loading() ? "Loading sessions" : state().title}</Text>
      {!loading() && state().action ? (
        <Button testID="sessions-empty-action" onPress={() => (state().action === "pair" ? props.onPair() : state().action === "retry" ? resumeConnection() : props.onNew())}
          class="rounded-full bg-primary px-4 py-2.5 active:opacity-80">
          <Text class="text-base font-semibold text-primary-foreground">{state().label}</Text>
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
export function Sessions(props: { nav: NavStackState<SessionsRoute>; onPair: () => void; onNew: () => void }) {
  const open = (worker: Worker) => {
    void markWorkerOpened(Storage, worker.id).catch(() => {});
    props.nav.push({ name: "session", workerId: worker.id, hint: { name: worker.name, cwd: worker.cwd, command: worker.command, startedAt: worker.started_at } });
  };
  const menu = async () => {
    const live = untracked(connection) === "connected";
    const { action } = await AlertDialog.show({
      title: "Sessions",
      presentation: "actionSheet",
      actions: [
        ...(live ? [{ id: "new", label: "New session" }, { id: "services", label: "Services" }] : []),
        { id: "cancel", label: "Cancel", role: "cancel" as const },
      ],
    });
    if (action === "new") props.onNew();
    else if (action === "services") servicesOpen.set(true);
  };
  // Android's Back ends selecting first (parity #26), as the Expo list did; a browser has its own back.
  if (platform.os !== "web") cleanup(BackHandler.on("press", () => {
    if (!untracked(selecting) || untracked(selectedTab) !== 1) return false;
    selection.set(EMPTY_SESSION_SELECTION);
    return true;
  }));
  // Leaving the tab ends selecting.
  effect(() => {
    if (selectedTab() !== 1 && untracked(selecting)) selection.set(EMPTY_SESSION_SELECTION);
  });
  // A Session that left the list cannot stay selected.
  effect(() => {
    const next = pruneSessionSelection(selection(), workers().map((w) => w.key));
    if (next !== untracked(selection)) selection.set(next);
  });
  return (
    <NavPage title="Sessions"
      headerRight={selecting() ? null : (
        <View class="flex-row items-center gap-4">
          <Button testID="sessions-menu" accessibilityLabel="Sessions menu" onPress={() => void menu()}><Icon name="more-horizontal" size={22} color="primary" /></Button>
          <Button testID="new-session" onPress={props.onNew} disabled={connection() !== "connected"}><Icon name="add" size={22} color="primary" /></Button>
        </View>
      )}>
      <View class="flex-1 bg-background">
        {selecting() ? (
          <View class="flex-row items-center gap-3 border-b border-border px-4 py-2">
            <Button testID="selection-cancel" onPress={() => selection.set(EMPTY_SESSION_SELECTION)}><Text class="text-base text-primary">Cancel</Text></Button>
            <Text class="flex-1 text-center text-base font-semibold">{selectionCountLabel(countSessionSelection(selection()))}</Text>
            <Button testID="selection-terminate" onPress={() => void terminateSelected()} disabled={terminating() || connection() !== "connected"}>
              <Text class="text-base font-semibold text-destructive">{terminating() ? "Terminating…" : "Terminate"}</Text>
            </Button>
          </View>
        ) : null}
        {notice() ? <Text testID="sessions-notice" class="px-4 py-2 text-sm text-destructive" onPress={() => notice.set("")}>{notice()}</Text> : null}
        <List keyboardShouldPersistTaps="handled" testID="sessions" class="flex-1" each={rows()} keyExtractor={(row) => row.key} contentClass="pb-24"
          empty={<Empty onPair={props.onPair} onNew={props.onNew} />}>
          {(row) => {
            // Cells are recycled: what a cell shows follows the row it holds now. A Session's update makes new
            // rows, so the cell is rebuilt only when it turns from a heading into a Session or back: a tap during
            // an update lands on the row, not on one being replaced. Each side keeps its last value while it goes.
            const header = computed(() => row().kind === "header");
            const section = computed<SessionListSection>((last) => (row().kind === "header" ? (row() as Extract<Row, { kind: "header" }>).section : last!));
            const worker = computed<Worker>((last) => (row().kind === "header" ? last! : (row() as Extract<Row, { kind: "row" }>).worker));
            return (
              <View>
                {header() ? <SectionHeader section={section} /> : <SessionRow worker={worker} open={open} />}
              </View>
            );
          }}
        </List>
      </View>
    </NavPage>
  );
}

/** Builds the key the device state and the route use for a Session of the current computer. */
export function currentSessionKey(workerId: string): string | null {
  const server = current();
  return server ? makeSessionKey(server.id, workerId) : null;
}
