import { elapsedNow } from "../kit/elapsedClock";
import { cleanup, computed, effect, Scroll, signal, Text, untracked, View } from "nsh";
import { Button, Input } from "nsh/base";
import { capsuleScrollTop, NavPage } from "../shell/NavPage";
import type { NavStackState } from "nsh/base";
import { markWorkRead, setWorkShown, workAction, workDetail, type WorkShownField } from "../../core/work/brainWork";
import type { BrainWorkDetail, BrainWorkStatus } from "../../core/work/types";
import { workDetailWorkerRow, workProject, workWorkerLines, type WorkWorkerLine } from "../../core/work/workers";
import { RowEnd, RowHair, RowText, RowTile } from "../tray/rowParts";
import {
  BRAIN_WORK_SNOOZE_SECONDS, brainWorkActions, brainWorkAskDraft, brainWorkUserAction, type BrainWorkAction,
} from "../../core/work/actions";
import { mergedWorkers, type BrainWorkSlip } from "../../core/work/surface";
import { progressBackLine, progressOutcomes, type BrainWorkProgress, type JudgedOutcomeState } from "../../core/work/judgedProgress";
import { OutcomeMark, ProgressHalo } from "../kit/ProgressHalo";
import { AgentIcon } from "../icons/AgentIcon";
import { WorkerScreenButton } from "../chat/TimelineRows";
import { clock } from "../calendar/clock";
import { createAttachmentUploads } from "../more/files/upload";
import { brainDraft, connection, currentTransport, workers } from "../session";
import { Composer, createComposerText, writeIntoDraft } from "../chat/Composer";
import { workFileOfWork, workSurface } from "./state";
import { Icon, type IconName } from "../kit/Icon";
import { delayed, RING, Spinner } from "../kit/wait";
import { haptic } from "../shell/haptics";
import { Caption, DISPLAY_TITLE_AWAY } from "../kit/kit";
import { AttachmentTray } from "../more/files/Attachments";
import { showAlert } from "../shell/alert";
import { bottomBarInset } from "../shell/below";
import { createMenuAnchor, showMenu, type MenuItem } from "../kit/Menu";
import { FileDrop } from "../chat/FileDrop";
import { attachIncoming, type IncomingFiles } from "../more/files/incoming";
import { AppSheet } from "../shell/AppSheet";

/** Where the Work pages lead outside themselves: a Session, the Calendar, Brain's composer. */
export interface WorkLinks {
  openSession(sessionId: string): void;
  /** The item, and its run when the Work came from one (v0.3.1's Calendar origin link). */
  openCalendarItem(itemId: string, runId?: string): void;
  /** "Ask Brain about this": Brain's composer with the Work quoted, never sent. */
  askBrain(text: string): void;
}

/** The action in flight ("<workId>:<kind>:<text>"): its button waits, the Work's others are off. */
const busy = signal("");
/** What the last action did, under the list's heading: "Sent to Brain", or why it failed. */
const notice = signal<{ text: string; detail?: string; failed?: boolean } | null>(null);
let noticeTimer: ReturnType<typeof setTimeout> | null = null;

function say(text: string, options: { detail?: string; failed?: boolean } = {}): void {
  notice.set({ text, ...options });
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => notice.set(null), 5_000);
}

const SAID: Partial<Record<BrainWorkAction["kind"], string>> = { close: "Closed", dismiss: "Dismissed", snooze: "Snoozed for 1 hour" };

interface ActionContext {
  links: WorkLinks;
  /** Opens the Work's page; `reply` puts the cursor in its reply box. */
  openDetail(workId: string, reply?: boolean): void;
  /** The reply box's words and files, on the Work's page. */
  reply?: { text: string; attachments: readonly unknown[] };
  /** After an action went through: the page closes. */
  done(): void;
}

/** One action of a Work, from its slip or its page: asks first when it cannot be undone, then tells Brain. */
async function runWorkAction(action: BrainWorkAction, slip: BrainWorkSlip, context: ActionContext): Promise<boolean> {
  const transport = currentTransport();
  if (action.kind === "ask") return context.links.askBrain(brainWorkAskDraft(slip)), false;
  if (action.kind === "open") return slip.sessionId ? context.links.openSession(slip.sessionId) : undefined, false;
  if (action.kind === "read") {
    if (transport) markWorkRead(transport, slip.workId);
    context.done();
    return true;
  }
  const words = context.reply?.text.trim() ?? "";
  if (action.kind === "reply" && !words && !context.reply?.attachments.length) return context.openDetail(slip.workId, true), false;
  if (action.confirm) {
    const answer = await showAlert({
      title: action.kind === "stop" ? `Stop “${slip.title}”?` : `${action.label}?`,
      description: action.confirm,
      actions: [{ id: "keep", label: "Cancel", role: "cancel" }, { id: "go", label: action.label }],
    });
    if (answer.action !== "go") return false;
  }
  const kind = brainWorkUserAction(action.kind);
  if (!kind) return false;
  if (!transport || transport.state !== "connected") return say("That didn't go through", { detail: "Daemon is not connected.", failed: true }), false;
  busy.set(`${slip.workId}:${action.kind}:${action.text ?? ""}`);
  try {
    const text = action.kind === "answer" ? action.text : action.kind === "reply" ? words : undefined;
    const result = await workAction(transport, slip.workId, kind, {
      text,
      attachments: action.kind === "reply" ? context.reply?.attachments : undefined,
      snoozeSeconds: kind === "snooze" ? BRAIN_WORK_SNOOZE_SECONDS : undefined,
    });
    if (kind === "reply") {
      if (result.admission === "uncertain") say("Sent, but Brain may not have it", { detail: "Check Brain's chat before sending again." });
      else say("Sent to Brain");
    } else say(action.kind === "stop" ? `Stopped “${slip.title}”` : SAID[action.kind] ?? "Done");
    // A request answered and acknowledged: the one light tick (SPEC §16.3 Haptics).
    if (action.kind === "answer") haptic.impact("light");
    context.done();
    return true;
  } catch (error) {
    say("That didn't go through", { detail: error instanceof Error ? error.message : undefined, failed: true });
    return false;
  } finally {
    busy.set("");
  }
}

function actionOff(action: BrainWorkAction, slip: BrainWorkSlip): boolean {
  return Boolean(busy() && busy().startsWith(`${slip.workId}:`)) || (connection() !== "connected" && action.kind !== "ask" && action.kind !== "open");
}
const actionTestID = (action: BrainWorkAction) => `work-action-${action.kind}${action.text ? `-${action.text}` : ""}`;
const actionBusy = (action: BrainWorkAction, slip: BrainWorkSlip) => busy() === `${slip.workId}:${action.kind}:${action.text ?? ""}`;

/** An answer to what the Work asks: a 54 pt row of its request card, with a ring; Brain's pick wears the request card's tag. */
function AnswerRow(props: { action: BrainWorkAction; slip: () => BrainWorkSlip; onPress: () => void }) {
  // Not optimistic (SPEC §16.2 pattern 8): the press, then the ring in the row's circle after `wait.quiet`.
  const ring = delayed(() => actionBusy(props.action, props.slip()));
  return (
    <Button testID={actionTestID(props.action)} disabled={actionOff(props.action, props.slip())} onPress={props.onPress}
      class={`min-h-[54px] flex-row items-center gap-3 border-t border-border px-4 active:bg-accent ${actionBusy(props.action, props.slip()) ? "" : "disabled:opacity-40"}`}>
      <Text class="min-w-0 flex-1 text-body">{props.action.label}</Text>
      <If when={props.action.pick}><Text testID="work-pick" class="text-caption text-muted-foreground">Brain's pick</Text></If>
      <View class="size-[22px] items-center justify-center">
        <If when={ring()} else={<View class="size-[22px] rounded-full border-2 border-faint" />}>
          <Spinner size={RING.button} label={props.action.label} />
        </If>
      </View>
    </Button>
  );
}

/**
 * The Workers (SPEC §11 Work detail): a group of tray rows, the tray's own parts (`workDetailWorkerRow`): the
 * agent's logo, the task name, what it does, its progress, its state at the end. Each pushes its Session; one
 * the list no longer has is shown but does not open.
 */
function Workers(props: { lines: () => WorkWorkerLine[]; project: () => string | undefined; onOpen: (sessionId: string) => void }) {
  const rows = () => props.lines().map((line) => ({ line, row: workDetailWorkerRow(line, elapsedNow(), props.project()) }));
  return (
    <View>
      <If when={props.lines().length}><>
          <Caption role="header" class="mb-2 mt-6 px-4" text="Workers" />
          <View testID="work-workers" class="overflow-hidden rounded-(--radius-group) bg-card">
            {rows().map(({ line, row }, index, list) => (
              <Button testID="work-worker" accessibilityLabel={[row.title, row.line, row.end].filter(Boolean).join(", ")} disabled={!line.openable} onPress={() => props.onOpen(line.sessionId)}
                class="min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 active:bg-accent">
                <RowTile tile={() => row.tile} />
                <RowText row={() => row} />
                <RowEnd row={() => row} />
                <If when={!(index === list.length - 1)}><RowHair /></If>
              </Button>
            ))}
          </View>
        </></If>
    </View>
  );
}

const OUTCOME_WORDS: Record<JudgedOutcomeState, string> = { done: "done", doing: "under way", todo: "to do" };

/**
 * Its outcomes (SPEC §11 Progress, Work detail): why it went back when it did, in amber; then the outcomes the
 * judge keeps, in its order, each marked done, under way or to do. None before a judgment.
 */
function Progress(props: { progress: () => BrainWorkProgress | undefined }) {
  const outcomes = () => progressOutcomes(props.progress());
  const back = () => progressBackLine(props.progress());
  return (
    <View>
      <If when={outcomes().length}><>
          <Caption role="header" class="mb-2 mt-6 px-4" text="Outcomes" />
          <View testID="work-progress" class="overflow-hidden rounded-(--radius-group) bg-card">
            <If when={back()}><View testID="work-progress-back" class="min-h-[44px] flex-row items-center gap-3 px-4 py-2.5">
                <View class="size-[20px] items-center justify-center"><View class="size-2 rounded-full bg-warning" /></View>
                <Text class="min-w-0 flex-1 text-sub font-semibold">{back()}</Text>
              </View></If>
            {outcomes().map((outcome, index) => (
              <View testID={`work-outcome-${outcome.state}`} accessibilityLabel={`${outcome.text}, ${OUTCOME_WORDS[outcome.state]}`}
                class={`min-h-[44px] flex-row items-center gap-3 px-4 py-2.5 ${index === 0 && !back() ? "" : "border-t border-border"}`}>
                <OutcomeMark state={() => outcome.state} />
                <Text class={`min-w-0 flex-1 text-sub ${outcome.state === "done" ? "text-muted-foreground" : ""}`}>{outcome.text}</Text>
              </View>
            ))}
          </View>
        </></If>
    </View>
  );
}

const CLOSED_STATUS: Partial<Record<BrainWorkStatus, string>> = { done: "Done", cancelled: "Cancelled" };

function Notice() {
  return (
    <View>
      <If when={notice()}><View testID="work-notice" class="gap-0.5 pt-3">
          <Text class="text-body font-semibold">{notice()!.text}</Text>
          <If when={notice()!.detail}><Text class="text-sub text-muted-foreground">{notice()!.detail!}</Text></If>
        </View></If>
    </View>
  );
}

/** "since 13:20" today, "since Nov 10" before. */
function since(at: string | undefined): string {
  const ms = at ? Date.parse(at) : NaN;
  if (!Number.isFinite(ms)) return "";
  const c = clock();
  const zone = c.zones.device;
  return `since ${c.zones.dateKey(ms, zone) === c.zones.dateKey(c.now, zone) ? c.format.time(ms, zone) : c.format.shortDate(ms, zone)}`;
}

/**
 * The top block (SPEC §11 Work detail): the eyebrow "Work · {project}", the title at display size, the state line
 * (a red dot and Needs you, a live dot and the running word, or a check and Done; then "· since 13:20").
 */
function TopBlock(props: { project: () => string | undefined; title: () => string; dot: () => "seal" | "live" | null; done: () => boolean; state: () => string; since: () => string }) {
  return (
    <View class="gap-1 px-1 pb-1 pt-3">
      <Caption testID="work-eyebrow" text={["Work", props.project()].filter(Boolean).join(" · ")} />
      <View selectable><Text testID="work-title" role="header" class="text-display font-bold tracking-display">{props.title()}</Text></View>
      <View testID="work-state" class="mt-1 flex-row items-center gap-2">
        <If when={props.dot()} else={<If when={props.done()}><Icon name="check" size={16} tone="soft" /></If>}><View class={`size-2 rounded-full ${props.dot() === "seal" ? "bg-seal" : "bg-foreground"}`} /></If>
        <Text class="min-w-0 flex-1 text-sub text-muted-foreground line-clamp-1">
          <Text class="font-semibold text-foreground">{props.state()}</Text>
          {props.since() ? ` · ${props.since()}` : ""}
        </Text>
      </View>
    </View>
  );
}

/**
 * The brief (SPEC §11 Work detail): what Brain wrote for the Worker, labelled as such and below what the judge wrote
 * for the person; a paragraph at 15/22, soft, four lines until tapped, then whole.
 */
function Brief(props: { text: () => string }) {
  const whole = signal(false);
  // Read in the JSX: a component runs once, and the brief arrives after the page opens.
  return <><If when={props.text()}><>
      <Caption role="header" class="mb-2 mt-6 px-4" text="Brief" />
      <View selectable testID="work-brief-toggle" onPress={() => whole.set(!whole())} class="px-1">
        <Text testID="work-brief" class={`text-sub leading-[22px] text-muted-foreground ${whole() ? "" : "line-clamp-4"}`}>{props.text()}</Text>
      </View>
    </></If></>;
}

/** One of Work detail's paragraphs under its label (Now, Next): the judge's words, in the person's language. */
function Paragraph(props: { testID: string; label: string; text: () => string }) {
  return <><If when={props.text()}><>
      <Caption role="header" class="mb-2 mt-6 px-4" text={props.label} />
      <View selectable class="rounded-(--radius-group) bg-card px-4 py-3"><Text testID={props.testID} class="text-body">{props.text()}</Text></View>
    </></If></>;
}

/** "14:05" today, "Nov 18 14:05" before. */
function momentTime(at: string): string {
  const ms = Date.parse(at);
  if (!Number.isFinite(ms)) return "";
  const c = clock();
  const zone = c.zones.device;
  const time = c.format.time(ms, zone);
  return c.zones.dateKey(ms, zone) === c.zones.dateKey(c.now, zone) ? time : `${c.format.shortDate(ms, zone)} ${time}`;
}

/**
 * Dated lines under a label (SPEC §11 Work detail): So far, each line the row read with when, and Asked, the
 * person's own messages that asked for it, quoted. Both in their words or the judge's, never Brain's notes.
 */
function Moments(props: { testID: string; label: string; items: () => readonly { at: string; text: string }[]; quoted?: boolean }) {
  return <><If when={props.items().length}><>
      <Caption role="header" class="mb-2 mt-6 px-4" text={props.label} />
      <View testID={props.testID} class="overflow-hidden rounded-(--radius-group) bg-card">
        {props.items().map((item, index) => (
          <View selectable testID={`${props.testID}-item`} class={`gap-0.5 px-4 py-2.5 ${index === 0 ? "" : "border-t border-border"}`}>
            <Text class="text-caption text-muted-foreground">{momentTime(item.at)}</Text>
            <Text class={`text-sub ${props.quoted ? "italic" : ""}`}>{props.quoted ? `“${item.text}”` : item.text}</Text>
          </View>
        ))}
      </View>
    </></If></>;
}

/**
 * What Brain asks (SPEC §11 Work detail, owner 2026-11-19): first on the page, the question and its answers, Brain's
 * pick tagged; the screen a Worker left for the person (a sign-in) is one tap from it. The dock below is the
 * free-text reply.
 */
function Ask(props: { slip: () => BrainWorkSlip; answers: () => BrainWorkAction[]; worker: () => string | undefined; onAnswer: (action: BrainWorkAction) => void }) {
  return (
    <View testID="work-ask" class="mt-4 overflow-hidden rounded-(--radius-group) bg-card">
      <View class="flex-row items-center gap-3 p-4">
        <View class="size-2 rounded-full bg-seal" />
        <View selectable class="min-w-0 flex-1"><Text testID="work-question" class="text-body font-semibold">{props.slip().question!}</Text></View>
        <WorkerScreenButton worker={props.worker} />
      </View>
      {props.answers().map((action) => <AnswerRow action={action} slip={props.slip} onPress={() => props.onAnswer(action)} />)}
    </View>
  );
}

/** A row of Work detail's own groups: an icon, the label, a value, a chevron when it opens something. */
function GroupRow(props: { testID: string; icon: IconName; label: string; value?: string; chevron?: boolean; strong?: boolean; disabled?: boolean; first: boolean; onPress: () => void; accessibilityLabel?: string }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.accessibilityLabel} disabled={props.disabled} onPress={props.onPress}
      class={`min-h-[56px] flex-row items-center gap-3 px-4 active:bg-accent disabled:opacity-40 ${props.first ? "" : "border-t border-border"}`}>
      <Icon name={props.icon} size={24} tone={props.strong ? "ink" : "soft"} />
      <Text class={`min-w-0 flex-1 text-body line-clamp-1 ${props.strong ? "font-semibold" : ""}`}>{props.label}</Text>
      <If when={props.value}>{(value) => <Text class="max-w-[50%] text-body text-muted-foreground line-clamp-1">{value()}</Text>}</If>
      <If when={props.chevron}><Icon name="chevron-right" size={18} tone="faint" /></If>
    </Button>
  );
}

const ACTION_ICON: Partial<Record<BrainWorkAction["kind"], IconName>> = {
  snooze: "time", stop: "stop", dismiss: "close", close: "check", read: "check", open: "terminal",
};

/** What the edit sheet sets: the row's title (Rename) or its project (Move to project). */
const EDIT_COPY: Record<"title" | "project", { heading: string; note: string; button: string }> = {
  title: { heading: "Rename", note: "Your name for this Work. It stays as you write it; empty lets Mewla name it again.", button: "Rename" },
  project: { heading: "Move to project", note: "The project this Work is listed under. It stays as you write it; empty lets Mewla choose again.", button: "Move" },
};

export function WorkDetail(props: { workId: string; nav: NavStackState<any>; links: WorkLinks }) {
  const slip = () => workSurface().slips.find((s) => s.workId === props.workId);
  // A Work no longer open: its page from the computer, with who worked on it.
  const closed = signal<BrainWorkDetail | null>(null);
  const closedFailure = signal("");
  let asked = false;
  effect(() => {
    const transport = connection() === "connected" ? currentTransport() : null;
    if (slip() || asked || !transport) return;
    asked = true;
    workDetail(transport, props.workId).then((work) => closed.set(work), (error) => closedFailure.set(error instanceof Error ? error.message : ""));
  });
  // The brief, So far and Asked (SPEC §11 Work detail): only the Work's own page carries them, so an open Work asks
  // for them when it opens and again when its line moves on.
  const openDetail = signal<BrainWorkDetail | null>(null);
  let detailFor: string | undefined;
  effect(() => {
    const transport = connection() === "connected" ? currentTransport() : null;
    const key = slip() ? slip()!.shownLine ?? "" : undefined;
    if (key === undefined || key === detailFor || !transport) return;
    detailFor = key;
    workDetail(transport, props.workId).then((work) => openDetail.set(work), () => {});
  });
  const detail = () => (slip() ? openDetail() : closed());
  const brief = () => detail()?.objective?.trim() ?? "";
  const soFar = () => detail()?.shown?.so_far ?? [];
  const askedWords = () => detail()?.shown?.asked ?? [];
  // Works the person merged into this one: their Workers are listed here too.
  const mergedHere = () => workSurface().slips.filter((other) => other.mergedInto === props.workId && other.workId !== props.workId);
  const sessions = computed(() => new Map(workers().map((w) => [w.id, w] as const)));
  const workerLines = () => workWorkerLines(slip() ? mergedWorkers(slip()!, mergedHere()) : closed()?.worker_sessions, sessions());
  const project = () => workProject(slip()?.project, workerLines());
  // Opening a result that came back reads it, from whichever way it was opened (the tray, a card, a notification).
  let read = false;
  effect(() => {
    const shown = slip();
    const transport = connection() === "connected" ? currentTransport() : null;
    if (read || !shown?.unread || !transport) return;
    read = true;
    markWorkRead(transport, shown.workId);
  });
  // ⋯ (mock round 2): Rename, Move to project, Merge with…; what the person sets there stays theirs.
  const menuAnchor = createMenuAnchor();
  const editing = signal<"title" | "project" | null>(null);
  const editValue = signal("");
  const mergeTargets = () => workSurface().slips.filter((other) => other.workId !== props.workId && !other.closed && !other.mergedInto);
  const menuItems = (): MenuItem[] => [
    { id: "rename", label: "Rename" },
    { id: "move", label: "Move to project" },
    ...(slip()?.mergedInto ? [{ id: "unmerge", label: "Show on its own" }] : mergeTargets().length && !mergedHere().length ? [{ id: "merge", label: "Merge with…" }] : []),
  ];
  const save = async (field: WorkShownField, value: string, done: string) => {
    const transport = connection() === "connected" ? currentTransport() : null;
    if (!transport) return say("Offline", { failed: true });
    try {
      await setWorkShown(transport, props.workId, field, value.trim());
      haptic.notification("success");
      say(done);
    } catch (error) {
      say("Not saved", { detail: error instanceof Error ? error.message : undefined, failed: true });
    }
  };
  const merge = async () => {
    const targets = untracked(mergeTargets);
    const target = await showMenu({ title: "Show this Work under", items: targets.map((other) => ({ id: other.workId, label: other.title })), anchor: menuAnchor.node(), testID: "work-merge-popover" });
    if (target) await save("merged_into", target, "Merged");
  };
  const menu = async () => {
    const action = await showMenu({ title: untracked(title), items: untracked(menuItems), anchor: menuAnchor.node(), testID: "work-menu-popover" });
    if (action === "rename" || action === "move") {
      const field = action === "rename" ? "title" : "project";
      editValue.set(field === "title" ? untracked(title) : untracked(project) ?? "");
      editing.set(field);
    } else if (action === "merge") await merge();
    else if (action === "unmerge") await save("merged_into", "", "Shown on its own");
  };
  const saveEdit = () => {
    const field = untracked(editing);
    if (!field) return;
    editing.set(null);
    void save(field, untracked(editValue), field === "title" ? "Renamed" : "Moved");
  };
  // The nav bar names the Work once its display title has scrolled away (SPEC §11).
  const titleAway = signal(false);
  const title = () => slip()?.title ?? closed()?.title ?? "";
  // On screen while nothing is pushed over it: only then does the dock lift for the keyboard.
  const depth = untracked(() => props.nav.routes?.().length ?? 0);
  const shown = () => (props.nav.routes?.().length ?? depth) === depth;

  // The dock (SPEC §11): the shared composer, its words and files kept on this page only.
  const text = createComposerText(() => null);
  const files = createAttachmentUploads();
  // Leaving the page drops what was picked and stops an upload still going.
  cleanup(() => files.dispose());
  const attachFailure = signal("");
  // An open page follows the Work; a successful action closes it, and the list says what happened.
  const context = (): ActionContext => ({
    links: props.links,
    openDetail: () => {},
    reply: { text: text.text(), attachments: files.ready() },
    done: () => {
      text.clear();
      files.clear();
      props.nav.pop();
    },
  });
  const attach = async () => {
    attachFailure.set("");
    try {
      await files.pickAndUpload();
    } catch (error) {
      attachFailure.set(error instanceof Error ? error.message : "Could not attach that file.");
    }
  };
  // Pasted into the field or dropped on the page while it has the composer: chips, as picked ones.
  const attachDropped = (incoming: IncomingFiles) => {
    attachFailure.set("");
    void attachIncoming(files, incoming).catch((error) => attachFailure.set(error instanceof Error ? error.message : "Could not attach that file."));
  };
  const uploading = () => files.items().some((item) => item.status !== "ready");

  const actions = () => brainWorkActions(slip()!);
  const answers = () => actions().filter((action) => action.kind === "answer");
  // Ask Brain about this is always there (it only fills Brain's composer); the table's own wording when it has one.
  const ask = (): BrainWorkAction => actions().find((action) => action.kind === "ask") ?? { kind: "ask", label: "Ask Brain about this" };
  // Snooze, Stop and the other ways to move the Work on. The dock replies; the Workers' rows open their Sessions.
  const moves = () => actions().filter((action) => action.kind !== "answer" && action.kind !== "reply" && action.kind !== "ask" && !(action.kind === "open" && workerLines().length));
  const file = () => (slip() ? workFileOfWork({ workId: slip()!.workId, runId: slip()!.calendar?.runId, sessionIds: [slip()!.sessionId, ...(slip()!.workerSessions ?? []).map((w) => w.session_id)].filter((id): id is string => Boolean(id)) }) : undefined);
  // The ink dot is for running Work only; Queued, Waiting, Back and the like say their word with no dot.
  const dot = (): "seal" | "live" | null => (slip()!.group === "needs" ? "seal" : slip()!.group === "running" ? "live" : null);
  // "N running" while Workers run (SPEC §11), else the Work's own word.
  const stateWord = () => {
    const running = workerLines().filter((line) => line.state === "running").length;
    return slip()!.group === "running" && running ? `${running} running` : slip()!.statusLabel;
  };
  // Since it began waiting, else since its first Worker joined; never updated_at, which moves on every sync.
  const startedAt = () => slip()!.attentionSince ?? (slip()!.workerSessions ?? []).map((w) => w.joined_at).filter(Boolean).sort()[0];
  // Asked something, the rest of the page is folded until opened.
  const detailsOpen = signal(false);
  // The Worker whose screen the ask may need (a sign-in it left open): its live one, else its only one.
  const askWorker = () => slip()?.sessionId ?? (workerLines().length === 1 ? workerLines()[0]!.sessionId : undefined);
  // The dock is made once while the Work is open: snapshots and the clock rebuild the slip, never the field under a reply.
  const docked = computed(() => Boolean(slip() && !slip()!.closed));
  const nowLine = () => (slip()?.closed && slip()!.shownStatus !== "done" ? "" : slip()?.shownLine) || slip()?.summary || "";

  return (
    <NavPage title={titleAway() ? title() : ""} testID="work-detail" capsuleUnder
      headerRight={<If when={slip()}><Button ref={menuAnchor.ref} testID="work-menu" accessibilityLabel="Work menu" onPress={() => void menu()} class="size-[44px] items-center justify-center rounded-full active:bg-accent">
          <Icon name="more-horizontal" size={22} tone="ink" />
        </Button></If>}>
      <FileDrop testID={`file-drop-work-${props.workId}`} class="flex-1 bg-background" onFiles={() => (docked() ? attachDropped : null)}>
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-4 pt-2" contentStyle={{ paddingTop: capsuleScrollTop() ? capsuleScrollTop() + 8 : undefined, paddingBottom: 32 + (docked() ? 0 : bottomBarInset()) }}
          onScroll={(e) => titleAway.set(e.offset.y > DISPLAY_TITLE_AWAY)} scrollEventThrottle={64}>
          <If when={!slip() && closed()} else={<If when={!slip()} else={<View>
              <TopBlock project={project} title={title} dot={dot} done={() => Boolean(slip()!.closed)} state={stateWord} since={() => since(startedAt())} />
              {/* What Brain asks comes first; what the Work is and who works on it waits below, folded (owner 2026-11-19). */}
              <If when={slip()!.question}><Ask slip={() => slip()!} answers={answers} worker={askWorker} onAnswer={(action) => void runWorkAction(action, slip()!, context())} /></If>
              <If when={!slip()!.question || detailsOpen()} else={
                <Button testID="work-details" accessibilityLabel="Details" onPress={() => detailsOpen.set(true)} class="mt-3 min-h-[44px] flex-row items-center gap-1 px-1 active:opacity-70">
                  <Text class="text-sub text-muted-foreground">Details</Text>
                  <Icon name="chevron-down" size={16} tone="soft" />
                </Button>}><View>
                {/* Its progress (§11 Progress): the ring around its Worker's logo (Brain's mark with several or none), no number. */}
                <If when={slip()!.mark}>{(mark) => <View class="px-1 pt-3"><View class="size-[56px] items-center justify-center">
                    <View class="absolute" style={{ left: 0, top: 0 }}><ProgressHalo mark={() => mark()!} size={56} testID="work-progress-ring" /></View>
                    <If when={workerLines().length === 1} else={<Icon name="brain" size={26} tone="ink" />}><AgentIcon kind={workerLines()[0]?.agent ?? "terminal"} flavor={workerLines()[0]?.flavor} size={28} /></If>
                  </View></View>}</If>
                {/* Where it stands and what comes next, as the judge wrote them in the person's language; Brain's notes never show (owner 2026-11-19). */}
                <Paragraph testID="work-summary" label="Now" text={() => (slip()!.question ? "" : nowLine())} />
                <Paragraph testID="work-next" label="Next" text={() => (slip()!.closed ? "" : slip()!.shownNext ?? "")} />
                <Progress progress={() => slip()!.progress} />
                <Moments testID="work-so-far" label="So far" items={soFar} />
                <Moments testID="work-asked" label="Asked" items={askedWords} quoted />
                <Workers lines={workerLines} project={project} onOpen={props.links.openSession} />
                <Brief text={brief} />
              </View></If>
              <View testID="work-links" class="mt-6 overflow-hidden rounded-(--radius-group) bg-card">
                <If when={file()}><GroupRow testID="work-file" icon="document-text" label="Work file" value={file()!.path.split("/").pop()} chevron first
                    onPress={() => props.nav.push({ name: "workFile", id: file()!.id })} /></If>
                <If when={slip()!.calendar && slip()!.calendarLink}><GroupRow testID="work-calendar-link" icon="calendar" label={slip()!.calendarLink!} accessibilityLabel={`From Calendar: ${slip()!.calendarLink!}`} chevron first={!file()}
                    onPress={() => props.links.openCalendarItem(slip()!.calendar!.itemId, slip()!.calendar!.runId || undefined)} /></If>
                <GroupRow testID={actionTestID(ask())} icon="chat" label={ask().label} chevron first={!file() && !(slip()!.calendar && slip()!.calendarLink)}
                  onPress={() => void runWorkAction(ask(), slip()!, context())} />
              </View>
              <If when={moves().length}><View testID="work-moves" class="mt-3 overflow-hidden rounded-(--radius-group) bg-card">
                  {moves().map((action, index) => (
                    <GroupRow testID={actionTestID(action)} icon={ACTION_ICON[action.kind] ?? "chevron-right"} label={actionBusy(action, slip()!) ? "…" : action.label}
                      strong={Boolean(action.primary || action.confirm)} disabled={actionOff(action, slip()!)} first={index === 0}
                      onPress={() => void runWorkAction(action, slip()!, context())} />
                  ))}
                </View></If>
              <Notice />
            </View>}><If when={closedFailure() || connection() !== "connected"}><View class="items-center gap-2 px-6 py-16">
                <Text class="text-title font-semibold tracking-title">{closedFailure() || "This Work is no longer open"}</Text>
              </View></If></If>}><View>
              <TopBlock project={project} title={title} dot={() => null} done={() => closed()!.status === "done"} state={() => CLOSED_STATUS[closed()!.status] ?? "Closed"} since={() => since(closed()!.updated_at)} />
              {/* How it ended, when the judge read it after it closed. */}
              <Paragraph testID="work-summary" label="How it ended" text={() => (closed()!.shown?.status === "done" ? closed()!.shown?.line?.trim() ?? "" : "")} />
              <Moments testID="work-so-far" label="So far" items={soFar} />
              <Moments testID="work-asked" label="Asked" items={askedWords} quoted />
              <Workers lines={workerLines} project={project} onOpen={props.links.openSession} />
              <Brief text={brief} />
            </View></If>
        </Scroll>
        <If when={docked()}><Composer text={text} placeholder={slip()?.question ? "Reply about this Work" : "Tell Brain about this Work"} active={shown}
            canSend={() => !busy() && !uploading()} attached={() => files.ready().length > 0}
            onFiles={attachDropped}
            disabled={() => connection() !== "connected"} disabledReason={() => "Offline"}
            running={() => false} stopping={() => false} canStop={() => false} onStop={() => {}}
            onSend={() => {
              void runWorkAction({ kind: "reply", label: "Reply" }, slip()!, context());
              // Kept until Brain has it: the page closes then, and a failure leaves the words to send again.
              return false;
            }}
            leading={
              <Button testID="work-attach" accessibilityLabel="Attach file" disabled={busy() !== "" || files.picking()} onPress={() => void attach()} class="h-[52px] w-[44px] items-center justify-center disabled:opacity-40">
                <Icon name="add" size={22} tone="soft" />
              </Button>
            }>
            <If when={files.items().length}><AttachmentTray uploads={files} /></If>
            <If when={attachFailure()}><View class="px-3 pb-1.5"><Text class="text-sub text-muted-foreground">{attachFailure()}</Text></View></If>
          </Composer></If>
      </FileDrop>
      <AppSheet open={editing() !== null} onOpenChange={({ open }) => { if (!open) editing.set(null); }} popupClass="rounded-t-2xl bg-background py-4 gap-3" sidePadding={16} backdropClass="bg-black/40">
        <If when={editing()}>{(field) => <View class="gap-3 pb-6">
            <Text class="text-lg font-semibold">{EDIT_COPY[field()].heading}</Text>
            <Text class="text-sm text-muted-foreground">{EDIT_COPY[field()].note}</Text>
            <Input testID="work-edit-input" value={editValue()} placeholder={field() === "title" ? title() : project() ?? ""} onChange={({ value }) => editValue.set(value)}
              class="rounded-xl border border-border bg-card px-3.5 py-2.5 text-base" />
            <View class="flex-row justify-end gap-4">
              <Button onPress={() => editing.set(null)}><Text class="text-base text-muted-foreground">Cancel</Text></Button>
              <Button testID="work-edit-save" onPress={saveEdit}><Text class="text-base font-semibold text-primary">{EDIT_COPY[field()].button}</Text></Button>
            </View>
          </View>}</If>
      </AppSheet>
    </NavPage>
  );
}

/** "Ask Brain about this": the quote joins Brain's composer, below anything typed there (SPEC §3.4). */
export function draftForBrain(text: string): void {
  writeIntoDraft(brainDraft(), text);
}
