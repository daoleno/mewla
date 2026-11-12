import { cleanup, computed, effect, Scroll, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import type { NavStackState } from "nsh/base";
import { markWorkRead, workAction, workDetail } from "../../core/work/brainWork";
import type { BrainWorkDetail, BrainWorkStatus } from "../../core/work/types";
import { workProject, workWorkerLines, type WorkWorkerLine } from "../../core/work/workers";
import { AgentIcon } from "../icons/AgentIcon";
import { ProgressBar } from "../kit/ProgressBar";
import {
  BRAIN_WORK_SNOOZE_SECONDS, brainWorkActions, brainWorkAskDraft, brainWorkUserAction, brainWorkWaitForLine, type BrainWorkAction,
} from "../../core/work/actions";
import type { BrainWorkSlip } from "../../core/work/surface";
import { clock } from "../calendar/clock";
import { createAttachmentUploads } from "../more/files/upload";
import { brainDraft, connection, currentTransport, workers } from "../session";
import { Composer, createComposerText, writeIntoDraft } from "../chat/Composer";
import { workFileOfWork, workSurface } from "./state";
import { Icon, type IconName } from "../kit/Icon";
import { Caption, DISPLAY_TITLE_AWAY } from "../kit/kit";
import { AttachmentTray } from "../more/files/Attachments";
import { showAlert } from "../shell/alert";

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

/** An answer to what the Work asks: a 54 pt row of its request card, with a ring. */
function AnswerRow(props: { action: BrainWorkAction; slip: () => BrainWorkSlip; onPress: () => void }) {
  return (
    <Button testID={actionTestID(props.action)} disabled={actionOff(props.action, props.slip())} onPress={props.onPress}
      class="min-h-[54px] flex-row items-center gap-3 border-t border-border px-4 active:bg-accent disabled:opacity-40">
      <Text class="min-w-0 flex-1 text-body">{actionBusy(props.action, props.slip()) ? "Sending…" : props.action.label}</Text>
      <View class="size-[22px] rounded-full border-2 border-faint" />
    </Button>
  );
}

/** A Worker of the Work (SPEC §11 Work detail): a tray row, "Agent · name", what it does, its progress, the time; pushes its Session. */
function WorkerRow(props: { line: WorkWorkerLine; first: boolean; onPress: () => void }) {
  const line = props.line;
  return (
    <Button testID="work-worker" accessibilityLabel={[`${line.agentLabel} · ${line.name}`, line.stateLabel, line.summary, line.time].filter(Boolean).join(", ")} disabled={!line.openable} onPress={props.onPress}
      class={`min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 active:bg-accent ${props.first ? "" : "border-t border-border"}`}>
      <View class="size-[28px] items-center justify-center"><AgentIcon kind={line.agent} flavor={line.flavor} size={20} /></View>
      <View class="min-w-0 flex-1">
        <Text class="text-body font-semibold line-clamp-1">{`${line.agentLabel} · ${line.name}`}</Text>
        {line.stateLabel || line.summary ? (
          <Text testID="work-worker-line" class="text-sub text-muted-foreground line-clamp-1">
            {line.stateLabel ? <Text class={`font-semibold ${line.state === "ended" ? "" : "text-foreground"}`}>{line.stateLabel}</Text> : null}
            {line.stateLabel && line.summary ? ` · ${line.summary}` : line.summary}
          </Text>
        ) : null}
        {line.progress !== undefined ? <ProgressBar filled={line.progress} testID="work-worker-progress" /> : null}
      </View>
      <Text class="text-caption text-muted-foreground">{line.time === "live" ? "" : line.time}</Text>
    </Button>
  );
}

function Workers(props: { lines: () => WorkWorkerLine[]; onOpen: (sessionId: string) => void }) {
  return (
    <View>
      {props.lines().length ? (
        <>
          <Caption role="header" class="mb-2 mt-6 px-4" text="Workers" />
          <View testID="work-workers" class="overflow-hidden rounded-(--radius-group) bg-card">
            {props.lines().map((line, index) => <WorkerRow line={line} first={index === 0} onPress={() => props.onOpen(line.sessionId)} />)}
          </View>
        </>
      ) : null}
    </View>
  );
}

const CLOSED_STATUS: Partial<Record<BrainWorkStatus, string>> = { done: "Done", cancelled: "Cancelled" };

function Notice() {
  return (
    <View>
      {notice() ? (
        <View testID="work-notice" class="gap-0.5 pt-3">
          <Text class="text-body font-semibold">{notice()!.text}</Text>
          {notice()!.detail ? <Text class="text-sub text-muted-foreground">{notice()!.detail!}</Text> : null}
        </View>
      ) : null}
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
function TopBlock(props: { project: () => string | undefined; title: () => string; dot: () => "seal" | "live" | null; state: () => string; since: () => string }) {
  return (
    <View class="gap-1 px-1 pb-1 pt-3">
      <Caption testID="work-eyebrow" text={["Work", props.project()].filter(Boolean).join(" · ")} />
      <View selectable><Text testID="work-title" role="header" class="text-display font-bold tracking-display">{props.title()}</Text></View>
      <View testID="work-state" class="mt-1 flex-row items-center gap-2">
        {props.dot() ? <View class={`size-2 rounded-full ${props.dot() === "seal" ? "bg-seal" : "bg-foreground"}`} /> : <Icon name="check" size={16} tone="soft" />}
        <Text class="min-w-0 flex-1 text-sub text-muted-foreground line-clamp-1">
          <Text class="font-semibold text-foreground">{props.state()}</Text>
          {props.since() ? ` · ${props.since()}` : ""}
        </Text>
      </View>
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
      {props.value ? <Text class="max-w-[50%] text-body text-muted-foreground line-clamp-1">{props.value}</Text> : null}
      {props.chevron ? <Icon name="chevron-right" size={18} tone="faint" /> : null}
    </Button>
  );
}

const ACTION_ICON: Partial<Record<BrainWorkAction["kind"], IconName>> = {
  snooze: "time", stop: "stop", dismiss: "close", close: "check", read: "check", open: "terminal",
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
  const sessions = computed(() => new Map(workers().map((w) => [w.id, w] as const)));
  const workerLines = () => workWorkerLines(slip() ? slip()!.workerSessions : closed()?.worker_sessions, sessions());
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
  const uploading = () => files.items().some((item) => item.status !== "ready");

  const actions = () => brainWorkActions(slip()!);
  const answers = () => actions().filter((action) => action.kind === "answer");
  // Ask Brain about this is always there (it only fills Brain's composer); the table's own wording when it has one.
  const ask = (): BrainWorkAction => actions().find((action) => action.kind === "ask") ?? { kind: "ask", label: "Ask Brain about this" };
  // Snooze, Stop and the other ways to move the Work on. The dock replies; the Workers' rows open their Sessions.
  const moves = () => actions().filter((action) => action.kind !== "answer" && action.kind !== "reply" && action.kind !== "ask" && !(action.kind === "open" && workerLines().length));
  const file = () => (slip() ? workFileOfWork({ workId: slip()!.workId, runId: slip()!.calendar?.runId, sessionIds: [slip()!.sessionId, ...(slip()!.workerSessions ?? []).map((w) => w.session_id)].filter((id): id is string => Boolean(id)) }) : undefined);
  const dot = (): "seal" | "live" | null => (slip()!.group === "needs" ? "seal" : slip()!.closed ? null : "live");

  return (
    <NavPage title={titleAway() ? title() : ""} testID="work-detail">
      <View class="flex-1 bg-background">
        <Scroll keyboardShouldPersistTaps="handled" class="flex-1" contentClass="px-4 pb-8 pt-2"
          onScroll={(e) => titleAway.set(e.offset.y > DISPLAY_TITLE_AWAY)} scrollEventThrottle={64}>
          {!slip() && closed() ? (
            <View>
              <TopBlock project={project} title={title} dot={() => null} state={() => CLOSED_STATUS[closed()!.status] ?? "Closed"} since={() => since(closed()!.updated_at)} />
              <Workers lines={workerLines} onOpen={props.links.openSession} />
            </View>
          ) : !slip() ? (
            // Nothing while the computer is asked; what it said, or offline, once there is no asking.
            closedFailure() || connection() !== "connected" ? (
              <View class="items-center gap-2 px-6 py-16">
                <Text class="text-title font-semibold tracking-title">{closedFailure() || "This Work is no longer open"}</Text>
              </View>
            ) : null
          ) : (
            <View>
              <TopBlock project={project} title={title} dot={dot} state={() => slip()!.statusLabel} since={() => since(slip()!.updatedAt)} />
              {slip()!.summary && !slip()!.question ? <View selectable><Text testID="work-question" class="px-1 pt-3 text-sub leading-[22px] text-muted-foreground">{slip()!.summary!}</Text></View> : null}
              {brainWorkWaitForLine(slip()!) ? <View selectable><Text testID="work-wait-for" class="px-1 pt-2 text-sub leading-[22px] text-muted-foreground">{brainWorkWaitForLine(slip()!)!}</Text></View> : null}
              {slip()!.question ? (
                <>
                  <Caption role="header" class="mb-2 mt-6 px-4" text="Waits on you" />
                  <View class="overflow-hidden rounded-(--radius-group) bg-card">
                    <View class="gap-2 p-4">
                      <View class="flex-row items-center gap-2">
                        <View class="size-2 rounded-full bg-seal" />
                        <Text class="text-caption font-semibold">Brain asks</Text>
                      </View>
                      <View selectable><Text testID="work-question" class="text-body font-semibold">{slip()!.question!}</Text></View>
                    </View>
                    {answers().map((action) => <AnswerRow action={action} slip={() => slip()!} onPress={() => void runWorkAction(action, slip()!, context())} />)}
                  </View>
                </>
              ) : null}
              <Workers lines={workerLines} onOpen={props.links.openSession} />
              <View testID="work-links" class="mt-6 overflow-hidden rounded-(--radius-group) bg-card">
                {file() ? (
                  <GroupRow testID="work-file" icon="document-text" label="Work file" value={file()!.path.split("/").pop()} chevron first
                    onPress={() => props.nav.push({ name: "workFile", id: file()!.id })} />
                ) : null}
                {slip()!.calendar && slip()!.calendarLink ? (
                  <GroupRow testID="work-calendar-link" icon="calendar" label={slip()!.calendarLink!} accessibilityLabel={`From Calendar: ${slip()!.calendarLink!}`} chevron first={!file()}
                    onPress={() => props.links.openCalendarItem(slip()!.calendar!.itemId, slip()!.calendar!.runId || undefined)} />
                ) : null}
                <GroupRow testID={actionTestID(ask())} icon="chat" label={ask().label} chevron first={!file() && !(slip()!.calendar && slip()!.calendarLink)}
                  onPress={() => void runWorkAction(ask(), slip()!, context())} />
              </View>
              {moves().length ? (
                <View testID="work-moves" class="mt-3 overflow-hidden rounded-(--radius-group) bg-card">
                  {moves().map((action, index) => (
                    <GroupRow testID={actionTestID(action)} icon={ACTION_ICON[action.kind] ?? "chevron-right"} label={actionBusy(action, slip()!) ? "…" : action.label}
                      strong={Boolean(action.primary || action.confirm)} disabled={actionOff(action, slip()!)} first={index === 0}
                      onPress={() => void runWorkAction(action, slip()!, context())} />
                  ))}
                </View>
              ) : null}
              <Notice />
            </View>
          )}
        </Scroll>
        {slip() && !slip()!.closed ? (
          <Composer text={text} placeholder="Reply about this Work" active={shown}
            canSend={() => !busy() && !uploading()} attached={() => files.ready().length > 0}
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
            {files.items().length ? <AttachmentTray uploads={files} /> : null}
            {attachFailure() ? <Text class="px-3 pb-1.5 text-sub text-muted-foreground">{attachFailure()}</Text> : null}
          </Composer>
        ) : null}
      </View>
    </NavPage>
  );
}

/** "Ask Brain about this": the quote joins Brain's composer, below anything typed there (SPEC §3.4). */
export function draftForBrain(text: string): void {
  writeIntoDraft(brainDraft(), text);
}
