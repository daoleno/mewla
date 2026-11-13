import { cleanup, computed, createMotion, effect, List, platform, signal, Storage, Text, untracked, View } from "nsh";
import { Button, type NavStackState } from "nsh/base";
import { DisplayTitle, PillButton, SectionLabel } from "../kit/kit";
import { BackHandler } from "nsh/mobile";
import { NavPage } from "../shell/NavPage";
import { makeSessionKey } from "../../core/sessions/sessionKeys";
import { sessionRowModel } from "../../core/sessions/listRow";
import { sessionRow } from "../../core/tray/model";
import { clock } from "../calendar/clock";
import { RowEnd, RowHair, RowText, RowTile } from "../tray/rowParts";
import { workLinkOf } from "./workLinks";
import { openPage } from "../shell/nav";
import { newSessionOpen } from "../screens/NewSession";
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
import { Icon, type IconName } from "../icons/Icon";
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

/**
 * A row knows where it sits in its folder's surface group: the first rounds the top, the last the bottom and
 * has no hair. Every folder has its label (SPEC §15 Grouped list); only several fold.
 */
type Row = { key: string; kind: "header"; section: SessionListSection; foldable: boolean } | { key: string; kind: "row"; worker: Worker; first: boolean; last: boolean };

const rows = computed<Row[]>(() => {
  const sections = buildSessionListSections(groupWorkersByDirectory(workers()), collapsed(), selecting());
  const out: Row[] = [];
  for (const section of sections) {
    out.push({ key: `h:${section.key}`, kind: "header", section, foldable: sections.length > 1 });
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
    },
  });
  batch.start();
}

/**
 * A folder's section label (SPEC §15 Grouped list): a folder icon, the name, the count at the right, inset
 * 32 like the tray's labels; with several folders a tap folds it, and a folded one with a Session waiting on
 * you keeps its red dot.
 */
function SectionHeader(props: { section: () => SessionListSection; foldable: () => boolean }) {
  const s = props.section;
  return (
    <View class="px-4">
      {props.foldable() ? (
        <SectionLabel
          testID="section"
          icon="folder"
          title={s().title}
          count={s().count}
          folded={s().collapsed}
          onPress={() => !selecting() && toggleSection(s().key)}
          trailing={s().mark === "needs" ? <View testID="section-needs" class="size-2 rounded-full bg-seal" /> : undefined}
        />
      ) : <SectionLabel testID="section" icon="folder" title={s().title} count={s().count} />}
    </View>
  );
}

/**
 * A Session as a tray row (SPEC §11, §15 Grouped list): 68 pt, its agent's logo, the title over what it is
 * doing in words, the end ("● 12m" while it runs); in its folder's surface group. Selecting, a check circle
 * takes the tile's place.
 */
function SessionRow(props: { worker: () => Worker; first: () => boolean; last: () => boolean; open: (worker: Worker) => void }) {
  const options = () => {
    const link = workLinkOf(props.worker());
    return { alias: aliases()[props.worker().id], workTitle: link?.workTitle, calendarLabel: link?.calendarLabel };
  };
  const row = () => sessionRow(props.worker(), clock().now, options());
  // Title, origin (From Calendar), state, what it does and when.
  const label = () => sessionRowModel(props.worker(), options()).accessibilityLabel;
  const selected = () => selection().has(props.worker().key);
  // Only a reachable computer can end its Sessions: offline rows can't be picked.
  const online = () => connection() === "connected";
  // A row new to the list fades in once; rows that scroll into view never do.
  const motion = untracked(() => freshRows().has(props.worker().key)) ? createMotion({ enter: "fadeIn", duration: 220 }) : null;
  return (
    <View testID="session-row" delayLongPress={400} style={motion?.style}
      accessibilityLabel={label()} role={selecting() ? "checkbox" : "button"}
      accessibilityState={selecting() ? { checked: selected(), disabled: !online() } : undefined}
      accessibilityHint={selecting() ? "Double tap to toggle selection" : "Opens the terminal session"}
      onPress={() => (selecting() ? online() && toggleSelected(props.worker()) : props.open(props.worker()))}
      onLongPress={() => {
        if (selecting() || !online()) return;
        toggleSelected(props.worker());
      }}
      onContextMenu={() => {
        // A right-click starts selecting, as a long press does on touch (parity #28, SPEC §14.4).
        if (!selecting() && online()) toggleSelected(props.worker());
      }}
      class={`mx-4 min-h-[68px] flex-row items-center gap-3.5 bg-card px-4 py-3 ${props.first() ? "rounded-t-(--radius-group)" : ""} ${props.last() ? "rounded-b-(--radius-group)" : ""} ${selecting() && !online() ? "opacity-50" : "active:bg-accent"}`}>
      {selecting() ? (
        <View testID="row-check" class="size-[28px] items-center justify-center">
          <Icon name={selected() ? "check-circle-fill" : "circle"} size={24} color={selected() ? "foreground" : "muted-foreground"} />
        </View>
      ) : <RowTile tile={() => row().tile} />}
      <RowText row={row} />
      {selecting() ? null : <RowEnd row={row} />}
      {props.last() ? null : <RowHair />}
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
        {notice() ? <View testID="sessions-notice" onPress={() => notice.set("")} class="px-4 py-2"><Text class="text-sub text-destructive">{notice()}</Text></View> : null}
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
            const foldable = computed<boolean>((last) => (row().kind === "header" ? (row() as Extract<Row, { kind: "header" }>).foldable : last ?? false));
            const item = computed<Extract<Row, { kind: "row" }>>((last) => (row().kind === "header" ? last! : (row() as Extract<Row, { kind: "row" }>)));
            return (
              <View>
                {header() ? <SectionHeader section={section} foldable={foldable} /> : <SessionRow worker={() => item().worker} first={() => item().first} last={() => item().last} open={open} />}
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
