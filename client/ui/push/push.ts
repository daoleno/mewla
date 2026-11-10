import { effect, host, lifecycle, locale, platform, Storage } from "nsh";
import { AlertDialog } from "nsh/base";
import { parseLink, pushLink } from "../../core/push/links";
import { liveAnswerOf, liveAnswerRequestId, widgetBaseUrl, type LiveAnswer } from "../../core/push/live";
import { ensurePermission, expoPushToken, registerPushMessage, type DeviceToken, type PushHost } from "../../core/push/registration";
import { NotificationReporter } from "../../core/push/report";
import type { StoredServer } from "../../core/storedServerContract";
import type { Transport } from "../../core/transport";
import { startCalendarAlerts } from "../calendar/alerts";
import { deviceZone } from "../calendar/clock";
import { calendarLinks, showCalendarItem } from "../calendar/routes";
import { clearCalendarAlerts, loadStrongAlerts, onStrongAlertsChange, strongAlerts } from "../calendar/state";
import { current, currentTransport, onTransport, ready, servers, switchTo } from "../session";
import { brainNav, selectedTab, openSession } from "../shell/Shell";
import { draftForBrain } from "../work/WorkPage";
import { workLinks } from "../work/routes";

/**
 * Notifications and the Live surfaces, for the one current computer: this phone's push
 * registration with it (on every connection), whether the app is in front (Watch decides
 * from it whether to push), a tapped push opening the page it is about, the live
 * notification / Live Activity and the home-screen widgets following its live state,
 * and the calendar's own alerts. Links from notifications, alerts and widgets all go
 * through `openNotificationLink`.
 */
const os = platform.os === "ios" || platform.os === "android" ? platform.os : null;
const hosted = host.available() && os !== null;

const pushHost: PushHost = {
  permission: () => host.call<{ granted: boolean }>("push.permission"),
  requestPermission: () => host.call<boolean>("push.requestPermission", undefined, { timeout: 0 }),
  deviceToken: () => host.call<DeviceToken>("push.deviceToken", undefined, { timeout: 60_000 }),
};

let token: Promise<string | null> | null = null;
/** This install's Expo token; asks for permission (once in the app's life) the first time a computer is connected. */
function expoToken(): Promise<string | null> {
  token ??= (async () => ((await ensurePermission(pushHost, Storage)) ? expoPushToken(pushHost, Storage, (input, init) => fetch(input, init)) : null))()
    .catch((error) => {
      console.warn("push not registered:", error instanceof Error ? error.message : error);
      return null;
    })
    .then((value) => {
      // Retry on the next connection: the permission may come later, a network error pass.
      if (!value) token = null;
      return value;
    });
  return token;
}

let liveCapable: Promise<boolean> | null = null;
const liveSupported = () => (liveCapable ??= host.call<boolean>("live.supported").then(Boolean, () => false));

async function register(transport: Transport, server: StoredServer): Promise<void> {
  const expo = await expoToken();
  if (!expo || !os || transport.state !== "connected") return;
  const message = registerPushMessage(server.id, expo, {
    timeZone: await deviceZone(),
    locale: locale(),
    platform: os,
    strongAlerts: strongAlerts(),
    liveState: await liveSupported(),
  });
  try {
    transport.send(message);
  } catch {
    // Dropped meanwhile: every connection registers again.
  }
}

function sendAppState(transport: Transport | null): void {
  if (transport?.state !== "connected") return;
  try {
    transport.send({ type: "app_state", app_state: lifecycle() });
  } catch {
    // The next connection reports it.
  }
}

const reporter = new NotificationReporter();

// One request at a time: the daemon keeps one widget token per device, so a second request would replace the first's.
let provisioning = false;

/** The live notification and widgets catch up with the computer's live state, and the widgets get their own token. */
async function syncLive(transport: Transport, server: StoredServer): Promise<void> {
  if (!(await liveSupported())) return;
  transport.request<{ live?: unknown }>("live_state_get", {}, { reply: "live_state", timeoutMs: 10_000 })
    .then((frame) => frame.live && host.call("live.apply", { state: JSON.stringify(frame.live) }), () => undefined);
  if (provisioning || (await host.call<string>("live.widgetState").catch(() => "ok")) !== "none") return;
  const base = widgetBaseUrl(server);
  if (!base) return void host.call("live.configureWidgetPushed").catch(() => {});
  provisioning = true;
  try {
    const frame = await transport.request<{ token?: string }>("widget_token_issue", {}, { reply: "widget_token", timeoutMs: 10_000 });
    if (frame.token && currentTransport() === transport) await host.call("live.configureWidget", { baseUrl: base, token: String(frame.token) });
  } catch {
    // The next connection asks again.
  } finally {
    provisioning = false;
  }
}

let pendingAnswer: LiveAnswer | null = null;

/** The answer a live notification's chip or a widget carries, sent once (a stable request id) when connected. */
function sendLiveAnswer(transport: Transport | null): void {
  const answer = pendingAnswer;
  if (!answer || transport?.state !== "connected") return;
  pendingAnswer = null;
  const requestId = liveAnswerRequestId(answer);
  const stops = [
    transport.on("live_answer", (frame) => frame.request_id === requestId && stops.forEach((stop) => stop())),
    transport.on("error", (frame) => {
      if (frame.request_id !== requestId) return;
      stops.forEach((stop) => stop());
      void AlertDialog.show({ title: "Couldn't send that answer", description: String(frame.message ?? ""), actions: [{ id: "ok", label: "OK" }] }).catch(() => {});
    }),
  ];
  try {
    transport.send({ type: "live_answer", request_id: requestId, question_id: answer.questionId, choice_index: answer.choiceIndex });
  } catch {
    pendingAnswer = answer;
    stops.forEach((stop) => stop());
  }
}

let waitingLink: string | null = null;

/**
 * Opens what a notification, an alert or a widget points at: its computer first (one
 * that is paired; another computer's link opens nothing of it), then the page.
 */
export function openNotificationLink(url: string): void {
  // A cold start's tap comes before the computers are loaded: it waits for them.
  if (!ready()) {
    waitingLink = url;
    return;
  }
  const link = parseLink(url);
  if (!link) return;
  const serverId = link.params.serverId;
  if (serverId && serverId !== current()?.id) {
    const target = servers().find((s) => s.id === serverId);
    if (!target) return;
    switchTo(target);
  }
  switch (link.section) {
    case "calendar":
      selectedTab.set(2);
      if (link.params.id) showCalendarItem(link.params.id);
      return;
    case "live":
      selectedTab.set(0);
      pendingAnswer = liveAnswerOf(link);
      sendLiveAnswer(currentTransport());
      return;
    case "brain":
      selectedTab.set(0);
      if (link.path[0] === "work") brainNav.setRoutes([{ name: "brain" }, { name: "work" }, ...(link.path[1] ? [{ name: "workDetail" as const, workId: link.path[1] }] : [])]);
      else brainNav.popToRoot();
      return;
    case "sessions":
      // mewla://sessions/<id> opens that Session of the current computer.
      if (link.path[0]) openSession(link.path[0]);
      else selectedTab.set(1);
      return;
  }
}

export function startNotifications(): void {
  const openSession = (sessionId: string) => openNotificationLink(`mewla://sessions/${encodeURIComponent(sessionId)}`);
  calendarLinks.openSession = openSession;
  workLinks.openSession = openSession;
  workLinks.openCalendarItem = (itemId) => openNotificationLink(`mewla://calendar?id=${encodeURIComponent(itemId)}`);
  workLinks.askBrain = (text) => {
    draftForBrain(text);
    brainNav.popToRoot();
  };
  startCalendarAlerts();
  effect(() => {
    if (!ready() || waitingLink === null) return;
    const url = waitingLink;
    waitingLink = null;
    openNotificationLink(url);
  });
  if (!hosted) return;

  void loadStrongAlerts();
  onTransport((transport, server) => {
    void host.call("live.setCurrentServer", { serverId: server.id }).catch(() => {});
    const connected = () => {
      sendAppState(transport);
      void register(transport, server);
      reporter.flush(server.id, (report) => transport.send({ ...report }));
      sendLiveAnswer(transport);
      void syncLive(transport, server);
    };
    const stops = [
      transport.on("connected", connected),
      transport.on("live_state", (frame) => {
        if (frame.live) void host.call("live.apply", { state: JSON.stringify(frame.live) }).catch(() => {});
      }),
      onStrongAlertsChange(() => void register(transport, server)),
    ];
    if (transport.state === "connected") connected();
    return () => stops.forEach((stop) => stop());
  });
  // No computer left: nothing of the last one may stay on the phone.
  effect(() => {
    if (current()) return;
    void host.call("live.setCurrentServer", { serverId: "" }).catch(() => {});
    clearCalendarAlerts();
  });
  effect(() => {
    lifecycle();
    sendAppState(currentTransport());
  });
  host.on<{ data?: Record<string, string> }>("push.opened", (payload) => {
    const data = payload?.data;
    const server = reporter.tapped(data);
    const transport = currentTransport();
    if (server && server === current()?.id && transport?.state === "connected") reporter.flush(server, (report) => transport.send({ ...report }));
    const url = pushLink(data);
    if (url) openNotificationLink(url);
  });
  host.on<{ url?: string }>("links.open", (payload) => {
    if (payload?.url && /^mewla:\/\/(calendar|live|brain|sessions)/i.test(payload.url)) openNotificationLink(payload.url);
  });
}
