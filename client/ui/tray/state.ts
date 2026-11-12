import { computed, signal, Storage } from "nsh";
import { brainSurface } from "../../core/brain";
import { openAlerts } from "../../core/calendar/alerts";
import { itemInstant, itemsOnDay, rowTime } from "../../core/calendar/model";
import { getRecentWorkerOpens } from "../../core/sessions/deviceState";
import { normalizePrompt } from "../../core/sessions/workers";
import { latestPendingChoice } from "../../core/timeline/claudeChoiceModel";
import type { SealedRequest } from "../../core/timeline/requestCards";
import { trayModel, type TraySort, type TrayTodayInput } from "../../core/tray/model";
import type { BrainCurrentWork, BrainOpenWork } from "../../core/work/types";
import { clock } from "../calendar/clock";
import { calendarItems } from "../calendar/state";
import { deviceRequests } from "../enrollment/state";
import { currentConnectionIssue, currentIssue } from "../more/settings/computers";
import { brain, brainThread, connection, current, problem, workers } from "../session";

const SORT_KEY = "mewla:tray:sort";

/** Running's order (the tray's ⋯ › Sort): last opened here, or last active. */
export const traySort = signal<TraySort>("opened");
void Storage.getItem(SORT_KEY).then((value) => {
  if (value === "activity" || value === "opened") traySort.set(value);
}).catch(() => {});

export function setTraySort(sort: TraySort): void {
  traySort.set(sort);
  void Storage.setItem(SORT_KEY, sort).catch(() => {});
}

/** When each Session was last opened here: read again whenever the tray opens. */
const opened = signal<Record<string, number>>({});
export function refreshOpened(): void {
  void getRecentWorkerOpens(Storage).then((value) => opened.set(value)).catch(() => {});
}
refreshOpened();

/**
 * Requests answered (or found passed) here, newest last: their cards seal to a quiet line in every
 * thread that shows them and their tray rows go in the same frame, before the computer clears them
 * (SPEC §1.1 "Sealed everywhere at once"). Kept while the app runs; keys are unique per prompt.
 */
export const sealedRequests = signal<readonly SealedRequest[]>([]);
const answered = computed(() => new Set(sealedRequests().map((sealed) => sealed.key)));

export function sealRequest(sealed: SealedRequest): void {
  sealedRequests.set([...sealedRequests().filter((s) => s.key !== sealed.key), sealed]);
}

/** Brain's own host Session, with the prompt it is blocked on. */
export const brainHost = computed(() => {
  const host = brain()?.host_worker;
  return host?.id ? { id: host.id, prompt: normalizePrompt(host.prompt) } : null;
});

/** Brain's pending chat question (AskUserQuestion), from its transcript. */
export const brainChoice = computed(() => {
  const pending = latestPendingChoice(brainThread().conversation?.events);
  return pending?.call_id ? { callId: pending.call_id, choice: pending.choice } : null;
});

function today(): TrayTodayInput[] {
  const now = clock();
  const listed = new Set(workers().map((worker) => worker.id));
  const key = now.zones.dateKey(now.now, now.zones.device);
  return itemsOnDay(calendarItems(), key, now).map((item) => {
    const run = item.runs?.at(-1);
    const sessionId = run?.status === "running" && run.worker_session && listed.has(run.worker_session) ? run.worker_session : undefined;
    return { id: item.id, title: item.title, time: rowTime(item, now), at: itemInstant(item), running: item.status === "running", sessionId };
  });
}

/** Home's tray and state control, from the current computer's state (one model: the pill and the tray agree). */
export const tray = computed(() => {
  const snapshot = brain();
  const server = current();
  const issue = currentIssue();
  return trayModel({
    hasServer: Boolean(server),
    computerName: server?.name ?? "",
    connection: connection(),
    pairAgain: issue === "device_not_paired",
    diagnosis: currentConnectionIssue()?.title || problem() || undefined,
    surface: brainSurface({ hasServer: Boolean(server), connected: connection() === "connected", snapshot }),
    hostProblem: snapshot?.host_problem,
    workers: workers(),
    brainHost: brainHost(),
    brainChoice: brainChoice() ? { callId: brainChoice()!.callId, question: brainChoice()!.choice.questions.find((q) => q.question)?.question ?? "" } : null,
    answered: answered(),
    currentWork: snapshot?.current_work as BrainCurrentWork[] | undefined,
    openWork: snapshot?.open_work as BrainOpenWork[] | undefined,
    today: today(),
    alerts: openAlerts(calendarItems(), clock().now).map((item) => ({ id: item.id, title: item.title })),
    devices: deviceRequests().map((request) => ({ id: request.id, name: request.deviceName })),
    opened: opened(),
    sort: traySort(),
    now: clock().now,
  });
});
