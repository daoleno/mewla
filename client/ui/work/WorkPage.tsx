import { List, Scroll, signal, Text, View } from "nsh";
import { AlertDialog, Button, Input, NavPage } from "nsh/base";
import type { NavStackState } from "nsh/base";
import { markWorkRead, objectiveLine, workAction } from "../../core/work/brainWork";
import { brainWorkActions, brainWorkAskDraft, brainWorkSnoozeUntil, brainWorkUserAction, type BrainWorkAction } from "../../core/work/actions";
import { BRAIN_WORK_GROUP_LABELS, BRAIN_WORK_GROUP_ORDER, brainWorkAge, brainWorkSummaryLine, type BrainWorkSlip, type WorkStatus } from "../../core/work/surface";
import { clock } from "../calendar/clock";
import { brainDraft, connection, currentTransport } from "../session";
import { workSurface } from "./state";

/** Brain's Work: what Brain is doing for you, grouped by what it asks of you. Not the Calendar's items. */
export type WorkRoute = { name: "work" } | { name: "workDetail"; workId: string };

/** Where the Work pages lead outside themselves: a Session, the Calendar, Brain's composer. */
export interface WorkLinks {
  openSession(sessionId: string): void;
  openCalendarItem(itemId: string): void;
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

function SlipRow(props: { slip: () => BrainWorkSlip; onPress: () => void }) {
  const s = () => props.slip();
  const summary = () => (s().group === "running" && s().phase ? `${s().phase![0].toUpperCase()}${s().phase!.slice(1)} · ${s().summary ?? ""}` : s().summary ?? "");
  return (
    <Button testID={`work-${s().workId}`} onPress={props.onPress} class="gap-1 border-b border-border bg-card px-4 py-3 active:bg-accent">
      <View class="flex-row items-center gap-2">
        <View class={`size-2 rounded-full ${DOT[s().status]}`} />
        <Text class="flex-1 text-sm text-muted-foreground line-clamp-1">{meta(s())}</Text>
        {s().statusLabel !== BRAIN_WORK_GROUP_LABELS[s().group] ? <Text class="text-sm text-muted-foreground">{s().statusLabel}</Text> : null}
        {s().unread ? <View accessibilityLabel="Unread" class="size-2 rounded-full bg-seal" /> : null}
      </View>
      <Text class="text-base font-semibold line-clamp-2">{s().title}</Text>
      {summary() ? <Text class="text-base text-muted-foreground line-clamp-2">{summary()}</Text> : null}
    </Button>
  );
}

export function WorkPage(props: { nav: NavStackState<any>; links: WorkLinks }) {
  const open = (slip: BrainWorkSlip) => {
    const transport = currentTransport();
    if (slip.unread && transport) markWorkRead(transport, slip.workId);
    if (slip.group === "running" && slip.sessionId && !slip.replied) return props.links.openSession(slip.sessionId);
    props.nav.push({ name: "workDetail", workId: slip.workId });
  };
  return (
    <NavPage title="Work" testID="work-page">
      <View class="flex-1 bg-background">
        <List testID="work-list" class="flex-1" each={rows()} keyExtractor={(row) => (row.kind === "group" ? `g:${row.title}` : `w:${row.slip.workId}`)}
          header={() => (
            <View class="gap-1 px-4 pb-1 pt-4">
              {objectiveLine(workSurface().objective) ? <Text class="text-base font-semibold">{objectiveLine(workSurface().objective)!}</Text> : null}
              {brainWorkSummaryLine(workSurface().counts) ? <Text testID="work-summary" class="text-sm text-muted-foreground">{brainWorkSummaryLine(workSurface().counts)!}</Text> : null}
            </View>
          )}
          empty={() => (
            <View testID="work-empty" class="items-center gap-2 px-6 py-16">
              <Text class="text-lg font-semibold">No open Work</Text>
              {connection() !== "connected" ? <Text class="text-center text-base text-muted-foreground">It shows when your computer is back.</Text> : null}
            </View>
          )}>
          {(row) => (
            <View>
              {row().kind === "group"
                ? <Text class="bg-background px-4 pb-1.5 pt-4 text-sm font-semibold text-muted-foreground">{(row() as { title: string }).title}</Text>
                : <SlipRow slip={() => (row() as { slip: BrainWorkSlip }).slip} onPress={() => open((row() as { slip: BrainWorkSlip }).slip)} />}
            </View>
          )}
        </List>
      </View>
    </NavPage>
  );
}

const SAID: Partial<Record<BrainWorkAction["kind"], string>> = { close: "Closed", stop: "Stopped", dismiss: "Dismissed", snooze: "Snoozed until tomorrow morning" };

export function WorkDetail(props: { workId: string; nav: NavStackState<any>; links: WorkLinks }) {
  const slip = () => workSurface().slips.find((s) => s.workId === props.workId);
  const reply = signal("");
  const busy = signal(false);
  const said = signal("");
  const failed = signal("");

  const run = async (action: BrainWorkAction, current: BrainWorkSlip) => {
    const transport = currentTransport();
    said.set("");
    failed.set("");
    if (action.kind === "ask") return props.links.askBrain(brainWorkAskDraft(current));
    if (action.kind === "open") return current.sessionId && props.links.openSession(current.sessionId);
    if (action.kind === "read") {
      if (transport) markWorkRead(transport, current.workId);
      return props.nav.pop();
    }
    if (action.kind === "reply" && !reply().trim()) return void said.set("Write your answer below, then Send reply.");
    if (action.confirm) {
      const answer = await AlertDialog.show({
        title: action.kind === "stop" ? `Stop “${current.title}”?` : `${action.label}?`,
        description: action.confirm,
        actions: [{ id: "keep", label: "Cancel", role: "cancel" }, { id: "go", label: action.label, role: "destructive" }],
      });
      if (answer.action !== "go") return;
    }
    const kind = brainWorkUserAction(action.kind);
    if (!kind || !transport) return void failed.set("Your computer is offline.");
    busy.set(true);
    try {
      const text = action.kind === "answer" ? action.text : action.kind === "reply" ? reply().trim() : undefined;
      const result = await workAction(transport, current.workId, kind, { text, snoozeUntil: kind === "snooze" ? brainWorkSnoozeUntil(clock().zones) : undefined });
      if (kind === "reply") {
        reply.set("");
        said.set(result.admission === "uncertain" ? "Sent, but Brain may not have it. Check Brain's chat before sending again." : "Brain has your answer");
      } else said.set(SAID[action.kind] ?? "Done");
    } catch (error) {
      failed.set(`That didn't go through. ${error instanceof Error ? error.message : ""}`.trim());
    } finally {
      busy.set(false);
    }
  };

  return (
    <NavPage title="Work" testID="work-detail">
      <Scroll class="flex-1 bg-background">
        {!slip() ? (
          <View class="items-center gap-2 px-6 py-16">
            <Text class="text-lg font-semibold">{said() || "This Work is no longer open"}</Text>
          </View>
        ) : (
          <View class="gap-3 px-4 pb-10 pt-4">
            <View class="flex-row items-center gap-2">
              <View class={`size-2 rounded-full ${DOT[slip()!.status]}`} />
              <Text class="text-sm text-muted-foreground">{[slip()!.statusLabel, meta(slip()!), slip()!.waitedMs && slip()!.group === "needs" ? `Waiting ${brainWorkAge(slip()!.waitedMs!)}` : ""].filter(Boolean).join(" · ")}</Text>
            </View>
            <Text testID="work-title" class="text-xl font-semibold">{slip()!.title}</Text>
            {slip()!.calendar && slip()!.calendarLink ? (
              <Button testID="work-calendar-link" onPress={() => props.links.openCalendarItem(slip()!.calendar!.itemId)} class="self-start"><Text class="text-base text-primary">{slip()!.calendarLink!}</Text></Button>
            ) : null}
            {slip()!.question || slip()!.summary ? <View selectable><Text testID="work-question" class="text-base">{slip()!.question ?? slip()!.summary!}</Text></View> : null}
            <View class="gap-2 pt-2">
              {brainWorkActions(slip()!).map((action) => (
                <Button testID={`work-action-${action.kind}${action.text ? `-${action.text}` : ""}`} disabled={busy() || (connection() !== "connected" && action.kind !== "ask" && action.kind !== "open")}
                  onPress={() => void run(action, slip()!)}
                  class={action.primary ? "rounded-xl bg-primary px-4 py-3" : "rounded-xl border border-border px-4 py-3 active:bg-accent"}>
                  <Text class={action.primary ? "text-center text-base font-semibold text-primary-foreground" : action.confirm ? "text-center text-base text-destructive" : "text-center text-base"}>{action.label}</Text>
                </Button>
              ))}
            </View>
            {!slip()!.closed ? (
              <View class="flex-row items-end gap-2 pt-2">
                <Input testID="work-reply" multiline value={reply()} onChange={({ value }) => reply.set(value)} placeholder={slip()!.question ? "Answer Brain…" : "Tell Brain about this…"}
                  class="max-h-40 flex-1 rounded-2xl border border-border bg-card px-3.5 py-2.5 text-base" />
                <Button testID="work-send-reply" accessibilityLabel="Send reply" disabled={busy() || !reply().trim()} onPress={() => void run({ kind: "reply", label: "Reply" }, slip()!)} class="rounded-full bg-primary px-4 py-2.5">
                  <Text class="text-base font-semibold text-primary-foreground">Send</Text>
                </Button>
              </View>
            ) : null}
            {said() ? <Text testID="work-said" class="text-base text-muted-foreground">{said()}</Text> : null}
            {failed() ? <Text testID="work-failed" class="text-base text-destructive">{failed()}</Text> : null}
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
