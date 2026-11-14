import { Button, TextView } from "nsh/base";
import { NavPage } from "../shell/NavPage";
import type { NavStackState } from "nsh/base";
import { effect, Scroll, signal, Text, View } from "nsh";
import {
  itemSummary, kindLabel, mergeRunResults, nextLine, runPreview, runsNewestFirst, runWhen, scheduleLine, statusLabel,
  type CalendarItem, type CalendarRun,
} from "../../core/calendar/model";
import { codexAsset } from "../../core/session/sessionRequests";
import { ROW_PICTURE_EDGE } from "../../core/timeline/pictureRetry";
import { ChatImage } from "../chat/ChatImage";
import { brain, currentTransport, workers } from "../session";
import { clock } from "./clock";
import { openEditor } from "./Editor";
import { Row, SectionTitle, StatusDot } from "./parts";
import { Icon } from "../kit/Icon";
import { LeadHero } from "../kit/RowTile";
import { calendarLead } from "../../core/calendar/colour";
import type { CalendarStatus } from "../../core/calendar/model";

/** A run's mark (SPEC §10.4): done is `check-circle` in ok, failed `alert-circle` in warning; the rest keep the dot. */
function RunMark(props: { status: CalendarStatus }) {
  return <If when={props.status === "completed"} else={<If when={props.status === "failed"} else={<StatusDot status={props.status} />}><Icon name="alert-circle" size={16} tone="warning" /></If>}><Icon name="check-circle" size={16} tone="ok" /></If>;
}
import type { Route } from "../shell/routes";
import { calendar, calendarItems } from "./state";
import { showAlert } from "../shell/alert";

const RUNS_SHOWN = 5;

/**
 * An image in a run's result, by its path on the computer: read as the same result reads in Brain's
 * conversation, in Brain's host Session's folder (a relative path is under it).
 */
function loadResultImage(path: string, full?: boolean): Promise<string | null> {
  const transport = currentTransport();
  if (!transport) return Promise.reject(new Error("No computer yet."));
  return codexAsset(transport, path, brain()?.host_worker?.cwd, full ? undefined : ROW_PICTURE_EDGE);
}

/** The item with its runs' full results, fetched for a scheduled action and again when it changes. */
function useFullItem(id: string) {
  const full = signal<CalendarItem | null>(null);
  let fetchedFor = "";
  effect(() => {
    const item = calendarItems().find((i) => i.id === id);
    const model = calendar();
    if (!item || !model || item.kind !== "scheduled_action") return;
    const version = `${item.updated_at}:${item.revision}`;
    if (version === fetchedFor) return;
    fetchedFor = version;
    void model.full(id).then((value) => full.set(value), () => undefined);
  });
  return () => {
    const item = calendarItems().find((i) => i.id === id);
    return item ? mergeRunResults(item, full()) : null;
  };
}

export function ItemPage(props: { id: string; nav: NavStackState<Route>; openSession: (sessionId: string) => void }) {
  const item = useFullItem(props.id);
  const busy = signal(false);
  const failure = signal("");
  const allRuns = signal(false);

  const act = async (work: () => Promise<unknown>) => {
    busy.set(true);
    failure.set("");
    try {
      await work();
    } catch (error) {
      failure.set(error instanceof Error ? error.message : "That did not go through.");
    } finally {
      busy.set(false);
    }
  };

  const cancel = async (target: CalendarItem) => {
    const answer = await showAlert({
      title: "Cancel calendar item?",
      description: "It will remain in Calendar as cancelled.",
      actions: [{ id: "keep", label: "Keep", role: "cancel" }, { id: "cancel", label: "Cancel item", role: "destructive" }],
    });
    // Cancelled, it stays in Calendar's history; the page goes back to the agenda.
    if (answer.action === "cancel") await act(async () => {
      await calendar()!.cancel(target);
      props.nav.pop();
    });
  };

  const header = (i: CalendarItem) => [scheduleLine(i, clock()), nextLine(i, clock())].filter(Boolean).join(" · ");
  const runs = () => runsNewestFirst(item() ?? {});
  /** An item-level failure, unless the newest run already says it. */
  const itemFailure = (i: CalendarItem) => (i.status === "failed" && i.failure_reason && runs()[0]?.failure_reason !== i.failure_reason ? i.failure_reason : "");

  return (
    <NavPage title={item()?.title ?? "Calendar"} testID="calendar-item-page">
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background">
        <If when={!item()} else={<View class="pb-10">
            <View class="gap-1.5 px-4 pb-2 pt-4">
              <LeadHero testID="item-lead" lead={calendarLead(item()!)} />
              <Text testID="item-title" class="text-xl font-semibold">{item()!.title}</Text>
              <Text testID="item-schedule" class="text-base text-muted-foreground">{header(item()!)}</Text>
              <View class="flex-row items-center gap-1.5">
                <StatusDot status={item()!.status} />
                <Text class="text-sm text-muted-foreground">{`${statusLabel[item()!.status]} · ${kindLabel[item()!.kind]}`}</Text>
              </View>
            </View>
            <If when={itemSummary(item()!)}><View class="px-4 py-2"><Text class="text-base">{itemSummary(item()!)}</Text></View></If>
            <If when={item()!.kind === "event" && item()!.end_at}><View class="px-4 py-1"><Text class="text-base text-muted-foreground">{`Ends ${clock().format.dateTime(Date.parse(item()!.end_at!), item()!.timezone, true)}`}</Text></View></If>
            <If when={item()!.notes}><View class="px-4 py-2"><Text class="text-base">{item()!.notes!}</Text></View></If>
            <If when={item()!.action_instruction}><View>
                <SectionTitle title="Instructions" />
                <View selectable class="px-4"><Text class="text-base text-muted-foreground">{item()!.action_instruction!}</Text></View>
              </View></If>
            <If when={itemFailure(item()!)}><View class="px-4 pt-3"><Text class="text-base text-warning">{itemFailure(item()!)}</Text></View></If>

            <If when={runs().length}><SectionTitle title="Runs" /></If>
            <View class="border-t border-border">
              {(allRuns() ? runs() : runs().slice(0, RUNS_SHOWN)).map((run) => (
                <Button testID={`run-${run.id}`}
                  // A run still running opens its Session while the computer lists it; the others, and one whose Session has gone, their result (parity #45).
                  onPress={() => (run.status === "running" && run.worker_session && workers().some((worker) => worker.id === run.worker_session)
                    ? props.openSession(run.worker_session) : props.nav.push({ name: "run", itemId: props.id, runId: run.id }))} class="gap-1 border-b border-border bg-card px-4 py-3 active:bg-accent">
                  <View class="flex-row items-center gap-1.5">
                    <RunMark status={run.status} />
                    <Text class="text-sm text-muted-foreground">{`${runWhen(run, item()!, clock())} · ${statusLabel[run.status]}`}</Text>
                  </View>
                  <If when={runPreview(run)}><Text class="text-base line-clamp-2">{runPreview(run)}</Text></If>
                </Button>
              ))}
              <If when={!allRuns() && runs().length > RUNS_SHOWN}><Row title={`Show all ${runs().length}`} onPress={() => allRuns.set(true)} /></If>
            </View>

            <View class="mt-6 border-t border-border">
              <If when={item()!.kind === "scheduled_action" && item()!.status !== "running" && item()!.status !== "cancelled"}><Row testID="run-now" title={item()!.status === "failed" ? "Retry now" : "Run now"} tone="primary" disabled={busy()} onPress={() => void act(() => calendar()!.runNow(props.id))} /></If>
              <Row testID="edit-item" title="Edit" disabled={busy() || item()!.status === "running"} onPress={() => openEditor(item())} />
              <Row testID="cancel-item" title="Cancel" tone="destructive" disabled={busy() || item()!.status === "running" || item()!.status === "cancelled"} onPress={() => void cancel(item()!)} />
            </View>
            <If when={failure()}><View class="px-4 pt-3"><Text testID="item-failure" class="text-base text-destructive">{`That did not go through. ${failure()}`}</Text></View></If>
          </View>}><View class="items-center px-6 py-16"><Text class="text-base text-muted-foreground">This item is no longer in Calendar.</Text></View></If>
      </Scroll>
    </NavPage>
  );
}

export function RunPage(props: { itemId: string; runId: string; nav: NavStackState<Route>; openSession: (sessionId: string) => void }) {
  // The result's width, which its pictures take.
  const resultWidth = signal(0);
  const item = useFullItem(props.itemId);
  const run = (): CalendarRun | undefined => item()?.runs?.find((r) => r.id === props.runId);
  return (
    <NavPage title="Run" testID="calendar-run-page">
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background">
        <If when={!run()} else={<View class="gap-3 px-4 pb-10 pt-4">
            <View class="flex-row items-center gap-1.5">
              <RunMark status={run()!.status} />
              <Text class="text-sm text-muted-foreground">{`${runWhen(run()!, item()!, clock())} · ${statusLabel[run()!.status]}${run()!.manual ? " · Run by you" : ""}`}</Text>
            </View>
            <If when={run()!.status === "failed"} else={<If when={run()!.result} else={<Text class="text-base text-muted-foreground">{run()!.result_preview ? `${run()!.result_preview}…` : run()!.status === "running" ? "Still running." : "No result."}</Text>}><View onLayout={({ bounds }) => resultWidth.set(bounds.size.width)}>
                    <TextView testID="run-result" markdown={run()!.result!} renderImage={(image) => <ChatImage src={image.src} alt={image.alt} load={loadResultImage} width={resultWidth} />} />
                  </View></If>}><View selectable><Text class="text-base text-warning">{run()!.failure_reason || "It failed without saying why."}</Text></View></If>
            {/* Its Session while the computer lists it, as the Expo app's run sheet did (parity #45). */}
            <If when={run()!.worker_session && workers().some((worker) => worker.id === run()!.worker_session)}><Button testID="open-session" onPress={() => props.openSession(run()!.worker_session!)} class="self-start rounded-full border border-border px-4 py-2 active:bg-accent">
                <Text class="text-base">Open Session</Text>
              </Button></If>
            <If when={run()!.work_id}><Button testID="open-work-file" onPress={() => props.nav.push({ name: "workFile", id: run()!.work_id! })} class="self-start rounded-full border border-border px-4 py-2 active:bg-accent">
                <Text class="text-base">Open its Work</Text>
              </Button></If>
          </View>}><View class="items-center px-6 py-16"><Text class="text-base text-muted-foreground">This run is no longer listed.</Text></View></If>
      </Scroll>
    </NavPage>
  );
}
