import { cleanup, computed, List, Scroll, signal, Text, View } from "nsh";
import { AlertDialog, Button, Input } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import type { NavStackState } from "nsh/base";
import { markWorkRead, objectiveLine, workAction } from "../../core/work/brainWork";
import {
  brainWorkActions, brainWorkAskDraft, brainWorkSlipActions, brainWorkSnoozeUntil, brainWorkUserAction, brainWorkWaitForLine, type BrainWorkAction,
} from "../../core/work/actions";
import { BRAIN_WORK_GROUP_LABELS, BRAIN_WORK_GROUP_ORDER, brainWorkAge, brainWorkSummaryLine, type BrainWorkSlip, type WorkStatus } from "../../core/work/surface";
import { clock } from "../calendar/clock";
import { createAttachmentUploads } from "../more/files/upload";
import { brainDraft, connection, currentTransport } from "../session";
import { workSurface } from "./state";

/** Brain's Work: what Brain is doing for you, grouped by what it asks of you. Not the Calendar's items. */
export type WorkRoute = { name: "work" } | { name: "workDetail"; workId: string };

/** Where the Work pages lead outside themselves: a Session, the Calendar, Brain's composer. */
export interface WorkLinks {
  openSession(sessionId: string): void;
  /** The item, and its run when the Work came from one (v0.3.1's Calendar origin link). */
  openCalendarItem(itemId: string, runId?: string): void;
  /** "Ask Brain about this": Brain's composer with the Work quoted, never sent. */
  askBrain(text: string): void;
}

type Row = { kind: "group"; title: string } | { kind: "slip"; slip: BrainWorkSlip };

function rows(): Row[] {
  const slips = workSurface().slips;
  return BRAIN_WORK_GROUP_ORDER.flatMap((group): Row[] => {
    const inGroup = slips.filter((s) => s.group === group);
    return inGroup.length ? [{ kind: "group", title: BRAIN_WORK_GROUP_LABELS[group] }, ...inGroup.map((slip) => ({ kind: "slip" as const, slip }))] : [];
  });
}

const DOT: Record<WorkStatus, string> = {
  needs: "bg-seal", running: "bg-ring", ready: "bg-muted-foreground", warning: "bg-warning", failed: "bg-destructive", blocked: "border border-muted-foreground",
};

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
      actions: [{ id: "keep", label: "Cancel", role: "cancel" }, { id: "go", label: action.label, role: "destructive" }],
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

function ActionButton(props: { action: BrainWorkAction; slip: () => BrainWorkSlip; onPress: () => void; compact?: boolean }) {
  const key = () => `${props.slip().workId}:${props.action.kind}:${props.action.text ?? ""}`;
  const off = () => Boolean(busy() && busy().startsWith(`${props.slip().workId}:`)) || (connection() !== "connected" && props.action.kind !== "ask" && props.action.kind !== "open");
  const pad = props.compact ? "px-3 py-1.5" : "px-4 py-3";
  return (
    <Button testID={`work-action-${props.action.kind}${props.action.text ? `-${props.action.text}` : ""}`} disabled={off()} onPress={props.onPress}
      class={props.action.primary ? `rounded-xl bg-primary ${pad}` : `rounded-xl border border-border ${pad} active:bg-accent`}>
      <Text class={props.action.primary ? "text-center text-base font-semibold text-primary-foreground" : props.action.confirm ? "text-center text-base text-destructive" : "text-center text-base"}>
        {busy() === key() ? "…" : props.action.label}
      </Text>
    </Button>
  );
}

function SlipRow(props: { slip: () => BrainWorkSlip; onPress: () => void; context: ActionContext }) {
  const s = () => props.slip();
  const summary = () => (s().group === "running" && s().phase ? `${s().phase![0].toUpperCase()}${s().phase!.slice(1)} · ${s().summary ?? ""}` : s().summary ?? "");
  return (
    <View class="border-b border-border bg-card">
      <Button testID={`work-${s().workId}`} onPress={props.onPress} class="gap-1 px-4 pb-2 pt-3 active:bg-accent">
        <View class="flex-row items-center gap-2">
          <View class={`size-2 rounded-full ${DOT[s().status]}`} />
          <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{meta(s())}</Text>
          {s().statusLabel !== BRAIN_WORK_GROUP_LABELS[s().group] ? <Text class="text-sm text-muted-foreground">{s().statusLabel}</Text> : null}
          {s().unread && s().group !== "needs" ? <View accessibilityLabel="Unread" class="size-2 rounded-full bg-seal" /> : null}
        </View>
        <Text class="text-base font-semibold line-clamp-2">{s().title}</Text>
        {summary() ? <Text class="text-base text-muted-foreground line-clamp-2">{summary()}</Text> : null}
      </Button>
      {/* The answers or the one next step, right on the slip; the rest on its page. */}
      <View class="flex-row flex-wrap gap-2 px-4 pb-3">
        {brainWorkSlipActions(s()).map((action) => (
          <ActionButton compact action={action} slip={s} onPress={() => void runWorkAction(action, s(), props.context)} />
        ))}
      </View>
    </View>
  );
}

function Notice() {
  return (
    <View>
      {notice() ? (
        <View testID="work-notice" class="gap-0.5 px-4 pt-3">
          <Text class={notice()!.failed ? "text-base font-semibold text-destructive" : "text-base font-semibold"}>{notice()!.text}</Text>
          {notice()!.detail ? <Text class="text-sm text-muted-foreground">{notice()!.detail!}</Text> : null}
        </View>
      ) : null}
    </View>
  );
}

export function WorkPage(props: { nav: NavStackState<any>; links: WorkLinks }) {
  const context: ActionContext = {
    links: props.links,
    openDetail: (workId) => props.nav.push({ name: "workDetail", workId }),
    done: () => {},
  };
  const open = (slip: BrainWorkSlip) => {
    const transport = currentTransport();
    if (slip.unread && transport) markWorkRead(transport, slip.workId);
    if (slip.group === "running" && slip.sessionId && !slip.replied) return props.links.openSession(slip.sessionId);
    props.nav.push({ name: "workDetail", workId: slip.workId });
  };
  return (
    <NavPage title="Work" testID="work-page">
      <View class="flex-1 bg-background">
        <List keyboardShouldPersistTaps="handled" testID="work-list" class="flex-1" each={rows()} keyExtractor={(row) => (row.kind === "group" ? `g:${row.title}` : `w:${row.slip.workId}`)}
          header={() => (
            <View>
              <Notice />
              <View class="gap-1 px-4 pb-1 pt-4">
                {objectiveLine(workSurface().objective) ? <Text class="text-base font-semibold">{objectiveLine(workSurface().objective)!}</Text> : null}
                {brainWorkSummaryLine(workSurface().counts) ? <Text testID="work-summary" class="text-sm text-muted-foreground">{brainWorkSummaryLine(workSurface().counts)!}</Text> : null}
              </View>
            </View>
          )}
          empty={() => (
            <View testID="work-empty" class="items-center gap-2 px-6 py-16">
              <Text class="text-lg font-semibold">No open Work</Text>
              {connection() !== "connected" ? <Text class="text-center text-base text-muted-foreground">It shows when your computer is back.</Text> : null}
            </View>
          )}>
          {(row) => {
            // Every Brain update makes new rows: the cell is rebuilt only when it turns from a heading into a slip or
            // back, so a tap is never lost to an update (as the Calendar agenda).
            const group = computed(() => row().kind === "group");
            // The slip row may read once more after the cell took a heading, before it goes: it keeps its last slip.
            const slip = computed<BrainWorkSlip>((last) => (row().kind === "group" ? last! : (row() as { slip: BrainWorkSlip }).slip));
            const title = computed<string>((last) => (row().kind === "group" ? (row() as { title: string }).title : last ?? ""));
            return (
              <View>
                {group()
                  ? <Text class="bg-background px-4 pb-1.5 pt-4 text-sm font-semibold text-muted-foreground">{title()}</Text>
                  : <SlipRow slip={slip} onPress={() => open(slip())} context={context} />}
              </View>
            );
          }}
        </List>
      </View>
    </NavPage>
  );
}

export function WorkDetail(props: { workId: string; nav: NavStackState<any>; links: WorkLinks }) {
  const slip = () => workSurface().slips.find((s) => s.workId === props.workId);
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

  return (
    <NavPage title="Work" testID="work-detail">
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background">
        {!slip() ? (
          <View class="items-center gap-2 px-6 py-16">
            <Text class="text-lg font-semibold">This Work is no longer open</Text>
          </View>
        ) : (
          <View class="gap-3 px-4 pb-10 pt-4">
            <View class="flex-row items-center gap-2">
              <View class={`size-2 rounded-full ${DOT[slip()!.status]}`} />
              <Text class="text-sm text-muted-foreground">{[slip()!.statusLabel, meta(slip()!), slip()!.waitedMs && slip()!.group === "needs" ? `Waiting ${brainWorkAge(slip()!.waitedMs!)}` : ""].filter(Boolean).join(" · ")}</Text>
            </View>
            <View selectable><Text testID="work-title" class="text-xl font-semibold">{slip()!.title}</Text></View>
            {slip()!.calendar && slip()!.calendarLink ? (
              <Button testID="work-calendar-link" accessibilityLabel={`From Calendar: ${slip()!.calendarLink!}`} onPress={() => props.links.openCalendarItem(slip()!.calendar!.itemId, slip()!.calendar!.runId || undefined)} class="self-start">
                <Text class="text-base text-primary">{slip()!.calendarLink!}</Text>
              </Button>
            ) : null}
            {slip()!.question || slip()!.summary ? <View selectable><Text testID="work-question" class="text-base">{slip()!.question ?? slip()!.summary!}</Text></View> : null}
            {brainWorkWaitForLine(slip()!) ? <View selectable><Text testID="work-wait-for" class="text-sm text-muted-foreground">{brainWorkWaitForLine(slip()!)!}</Text></View> : null}
            <View class="gap-2 pt-2">
              {brainWorkActions(slip()!).filter((action) => !(action.kind === "reply" && slip()!.closed)).map((action) => (
                <ActionButton action={action} slip={() => slip()!} onPress={() => void runWorkAction(action, slip()!, context())} />
              ))}
            </View>
            {!slip()!.closed ? (
              <View class="gap-2 pt-2">
                {files.items().length ? (
                  <View class="flex-row flex-wrap gap-2">
                    {files.items().map((item) => (
                      <Button accessibilityLabel={`Remove ${item.asset.name}`} onPress={() => files.remove(item.id)} class="rounded-full border border-border px-3 py-1">
                        <Text class={item.status === "failed" ? "text-sm text-destructive" : "text-sm"}>{item.status === "ready" ? `${item.asset.name} ×` : item.status === "failed" ? `${item.asset.name}: ${item.error ?? "failed"} ×` : `${item.asset.name} …`}</Text>
                      </Button>
                    ))}
                  </View>
                ) : null}
                <View class="flex-row items-end gap-2">
                  <Button testID="work-attach" accessibilityLabel="Attach file" disabled={busy() !== "" || files.picking()} onPress={() => void attach()} class="rounded-full border border-border px-3 py-2.5 active:bg-accent">
                    <Text class="text-base">+</Text>
                  </Button>
                  <Input testID="work-reply" multiline value={reply()} onChange={({ value }) => reply.set(value)} placeholder={slip()!.question ? "Answer Brain…" : "Tell Brain about this…"}
                    accessibilityLabel={`Reply to Brain about ${slip()!.title}`} class="max-h-40 flex-1 rounded-2xl border border-border bg-card px-3.5 py-2.5 text-base" />
                  <Button testID="work-send-reply" accessibilityLabel="Send reply" disabled={!canSend()} onPress={() => void runWorkAction({ kind: "reply", label: "Reply" }, slip()!, context())} class="rounded-full bg-primary px-4 py-2.5">
                    <Text class="text-base font-semibold text-primary-foreground">Send</Text>
                  </Button>
                </View>
                {attachFailure() ? <Text class="text-sm text-destructive">{attachFailure()}</Text> : null}
              </View>
            ) : null}
            <Notice />
          </View>
        )}
      </Scroll>
    </NavPage>
  );
}

/** The Brain header's way into Work: "Work", with how many need you. */
export function WorkHeaderButton(props: { onPress: () => void }) {
  return (
    <Button testID="open-work" onPress={props.onPress} accessibilityLabel="Work">
      <Text class="text-base text-primary">{workSurface().counts.needs ? `Work · ${workSurface().counts.needs}` : "Work"}</Text>
    </Button>
  );
}

/** "Ask Brain about this" until Brain's composer takes text from outside: the thread's draft holds it. */
export function draftForBrain(text: string): void {
  brainDraft()?.save(text);
}
