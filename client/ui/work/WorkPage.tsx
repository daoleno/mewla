import { cleanup, effect, Scroll, signal, Text, View } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import type { NavStackState } from "nsh/base";
import { markWorkRead, workAction } from "../../core/work/brainWork";
import {
  brainWorkActions, brainWorkAskDraft, brainWorkSnoozeUntil, brainWorkUserAction, brainWorkWaitForLine, type BrainWorkAction,
} from "../../core/work/actions";
import { brainWorkAge, type BrainWorkSlip } from "../../core/work/surface";
import { clock } from "../calendar/clock";
import { createAttachmentUploads } from "../more/files/upload";
import { brainDraft, connection, currentTransport } from "../session";
import { workSurface } from "./state";
import { Icon } from "../kit/Icon";

/** Where the Work pages lead outside themselves: a Session, the Calendar, Brain's composer. */
export interface WorkLinks {
  openSession(sessionId: string): void;
  /** The item, and its run when the Work came from one (v0.3.1's Calendar origin link). */
  openCalendarItem(itemId: string, runId?: string): void;
  /** "Ask Brain about this": Brain's composer with the Work quoted, never sent. */
  askBrain(text: string): void;
}

function meta(slip: BrainWorkSlip): string {
  const at = slip.updatedAt ? Date.parse(slip.updatedAt) : NaN;
  const when = Number.isFinite(at) ? clock().format.shortDate(at, clock().zones.device) : "";
  return [slip.calendarLabel ?? slip.who, when].filter(Boolean).join(" · ");
}

/** The action in flight ("<workId>:<kind>:<text>"): its button waits, the Work's others are off. */
const busy = signal("");
/** What the last action did, under the list's heading: "Brain has your answer", or why it failed. */
const notice = signal<{ text: string; detail?: string; failed?: boolean } | null>(null);
let noticeTimer: ReturnType<typeof setTimeout> | null = null;

function say(text: string, options: { detail?: string; failed?: boolean } = {}): void {
  notice.set({ text, ...options });
  if (noticeTimer) clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => notice.set(null), 5_000);
}

const SAID: Partial<Record<BrainWorkAction["kind"], string>> = { close: "Closed", stop: "Stopped", dismiss: "Dismissed", snooze: "Snoozed until tomorrow morning" };

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
    const answer = await AlertDialog.show({
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
      snoozeUntil: kind === "snooze" ? brainWorkSnoozeUntil(clock().zones) : undefined,
    });
    if (kind === "reply") {
      if (result.admission === "uncertain") say("Sent, but Brain may not have it", { detail: "Check Brain's chat before sending again." });
      else say("Brain has your answer");
    } else say(SAID[action.kind] ?? "Done");
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

/** Any other action: a 56 pt row of a group (Stop and the like in ink, as every destructive row). */
function ActionRow(props: { action: BrainWorkAction; slip: () => BrainWorkSlip; onPress: () => void; first: boolean }) {
  return (
    <Button testID={actionTestID(props.action)} disabled={actionOff(props.action, props.slip())} onPress={props.onPress}
      class={`min-h-[56px] flex-row items-center px-4 active:bg-accent disabled:opacity-40 ${props.first ? "" : "border-t border-border"}`}>
      <Text class={`min-w-0 flex-1 text-body ${props.action.primary || props.action.confirm ? "font-semibold" : ""}`}>{actionBusy(props.action, props.slip()) ? "…" : props.action.label}</Text>
      {props.action.confirm ? null : <Icon name="chevron-right" size={18} tone="faint" />}
    </Button>
  );
}

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

export function WorkDetail(props: { workId: string; nav: NavStackState<any>; links: WorkLinks }) {
  const slip = () => workSurface().slips.find((s) => s.workId === props.workId);
  // Opening a result that came back reads it, from whichever way it was opened (the tray, a card, a notification).
  let read = false;
  effect(() => {
    const shown = slip();
    const transport = connection() === "connected" ? currentTransport() : null;
    if (read || !shown?.unread || !transport) return;
    read = true;
    markWorkRead(transport, shown.workId);
  });
  const reply = signal("");
  const files = createAttachmentUploads();
  // Leaving the page drops what was picked and stops an upload still going.
  cleanup(() => files.dispose());
  const attachFailure = signal("");
  // An open page follows the Work; a successful action closes it, and the list says what happened.
  const context = (): ActionContext => ({
    links: props.links,
    openDetail: () => {},
    reply: { text: reply(), attachments: files.ready() },
    done: () => {
      reply.set("");
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
  const canSend = () => !busy() && !uploading() && Boolean(reply().trim() || files.ready().length);

  const answers = () => brainWorkActions(slip()!).filter((action) => action.kind === "answer");
  const others = () => brainWorkActions(slip()!).filter((action) => action.kind !== "answer" && !(action.kind === "reply" && (slip()!.closed || Boolean(slip()!.question))));
  const stateDot = () => (slip()!.group === "needs" ? "bg-seal" : slip()!.group === "running" ? "bg-foreground" : "");

  return (
    <NavPage title="Work" testID="work-detail">
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background">
        {!slip() ? (
          <View class="items-center gap-2 px-6 py-16">
            <Text class="text-title font-semibold tracking-title">This Work is no longer open</Text>
          </View>
        ) : (
          <View class="gap-2 px-4 pb-10 pt-2">
            <Text class="text-caption font-semibold text-muted-foreground">{["Work", slip()!.who].filter(Boolean).join(" · ")}</Text>
            <View selectable><Text testID="work-title" role="header" class="text-display font-bold tracking-display">{slip()!.title}</Text></View>
            <View class="flex-row items-center gap-2">
              {stateDot() ? <View class={`size-2 rounded-full ${stateDot()}`} /> : <Icon name="check" size={16} tone="soft" />}
              <Text class="text-sub text-muted-foreground">
                <Text class="font-semibold text-foreground">{slip()!.statusLabel}</Text>
                {[meta(slip()!), slip()!.waitedMs && slip()!.group === "needs" ? `Waiting ${brainWorkAge(slip()!.waitedMs!)}` : ""].filter(Boolean).map((part) => ` · ${part}`).join("")}
              </Text>
            </View>
            {slip()!.summary && !slip()!.question ? <View selectable><Text testID="work-question" class="pt-2 text-sub text-muted-foreground">{slip()!.summary!}</Text></View> : null}
            {brainWorkWaitForLine(slip()!) ? <View selectable><Text testID="work-wait-for" class="text-sub text-muted-foreground">{brainWorkWaitForLine(slip()!)!}</Text></View> : null}
            {slip()!.question ? (
              <>
                <Text role="header" class="mb-2 mt-6 px-4 text-caption font-semibold text-muted-foreground">Waits on you</Text>
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
            {slip()!.calendar && slip()!.calendarLink ? (
              <View class="mt-6 overflow-hidden rounded-(--radius-group) bg-card">
                <Button testID="work-calendar-link" accessibilityLabel={`From Calendar: ${slip()!.calendarLink!}`} onPress={() => props.links.openCalendarItem(slip()!.calendar!.itemId, slip()!.calendar!.runId || undefined)}
                  class="min-h-[56px] flex-row items-center gap-3 px-4 active:bg-accent">
                  <Icon name="calendar" size={24} tone="soft" />
                  <Text class="min-w-0 flex-1 text-body line-clamp-1">{slip()!.calendarLink!}</Text>
                  <Icon name="chevron-right" size={18} tone="faint" />
                </Button>
              </View>
            ) : null}
            {others().length ? (
              <View class="mt-6 overflow-hidden rounded-(--radius-group) bg-card">
                {others().map((action, index) => <ActionRow action={action} slip={() => slip()!} first={index === 0} onPress={() => void runWorkAction(action, slip()!, context())} />)}
              </View>
            ) : null}
            <Notice />
            {!slip()!.closed ? (
              <View class="gap-2 pt-6">
                {files.items().length ? (
                  <View class="flex-row flex-wrap gap-2">
                    {files.items().map((item) => (
                      <Button accessibilityLabel={`Remove ${item.asset.name}`} onPress={() => files.remove(item.id)} class="h-[44px] justify-center rounded-(--radius-control) bg-card px-3">
                        <Text class="text-sub">{item.status === "ready" ? `${item.asset.name} ×` : item.status === "failed" ? `${item.asset.name}: ${item.error ?? "failed"} ×` : `${item.asset.name} …`}</Text>
                      </Button>
                    ))}
                  </View>
                ) : null}
                <View class="min-h-[52px] flex-row items-end rounded-full bg-card">
                  <Button testID="work-attach" accessibilityLabel="Attach file" disabled={busy() !== "" || files.picking()} onPress={() => void attach()} class="h-[52px] w-[44px] items-center justify-center disabled:opacity-40">
                    <Icon name="add" size={22} tone="soft" />
                  </Button>
                  <Input testID="work-reply" multiline value={reply()} onChange={({ value }) => reply.set(value)} placeholder={slip()!.question ? "Something else…" : "Reply about this Work"}
                    accessibilityLabel={`Reply to Brain about ${slip()!.title}`} class="max-h-32 min-h-[52px] flex-1 py-3.5 text-message" />
                  <Button testID="work-send-reply" accessibilityLabel="Send reply" disabled={!canSend()} onPress={() => void runWorkAction({ kind: "reply", label: "Reply" }, slip()!, context())}
                    class="size-[52px] items-center justify-center disabled:opacity-40">
                    <View class={`size-9 items-center justify-center rounded-full ${canSend() ? "bg-primary" : "bg-background"}`}>
                      <Icon name="arrow-up" size={20} tone={canSend() ? "surface" : "faint"} />
                    </View>
                  </Button>
                </View>
                {attachFailure() ? <Text class="text-sub">{attachFailure()}</Text> : null}
              </View>
            ) : null}
          </View>
        )}
      </Scroll>
    </NavPage>
  );
}

/** "Ask Brain about this" until Brain's composer takes text from outside: the thread's draft holds it. */
export function draftForBrain(text: string): void {
  brainDraft()?.save(text);
}
