import { effect, host, locale, platform, Storage, lifecycle } from "nsh";
import { parseLink, pushLink } from "../../core/push/links";
import { liveAnswerOf, liveAnswerRequestId, widgetBaseUrl, type LiveAnswer } from "../../core/push/live";
import { ensurePermission, expoPushToken, registerPushMessage, type DeviceToken, type PushHost } from "../../core/push/registration";
import { NotificationReporter } from "../../core/push/report";
import type { StoredServer } from "../../core/storedServerContract";
import type { Transport } from "../../core/transport";
import { startCalendarAlerts } from "../calendar/alerts";
import { deviceZone } from "../calendar/clock";
import { refreshReminders, reminderLinks, startReminderBanner } from "../calendar/ReminderBanner";
import { closeEditor } from "../calendar/Editor";
import { clearCalendarAlerts, loadStrongAlerts, onStrongAlertsChange, strongAlerts } from "../calendar/state";
import { current, currentTransport, onTransport, openBrainThread, ready, servers, switchTo } from "../session";
import { goHome, openPage } from "../shell/nav";
import { draftForBrain } from "../work/WorkPage";
import { workLinks } from "../work/links";
import { showAlert } from "../shell/alert";

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
  token ??= (async () => {
    const allowed = await ensurePermission(pushHost, Storage);
    refreshReminders();
    return allowed ? expoPushToken(pushHost, Storage, (input, init) => fetch(input, init)) : null;
  })()
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
  const failed = (message: string) => {
    stops.forEach((stop) => stop());
    void showAlert({ title: "Couldn't send that answer", description: message, actions: [{ id: "ok", label: "OK" }] }).catch(() => {});
  };
  const timer = setTimeout(() => failed("Timed out. The answer may still have reached Brain."), 30_000);
  const stops = [
    () => clearTimeout(timer),
    transport.on("live_answer", (frame) => frame.request_id === requestId && stops.forEach((stop) => stop())),
    transport.on("error", (frame) => {
      if (frame.request_id === requestId) failed(String(frame.message || "That answer could not be sent."));
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
    // The editor goes first: switching under an open sheet crashes NSH 0.4.0's runtime (SIGSEGV in `nsh test`).
    closeEditor();
    // Open the link once the switch is done (it waits for open sheets to close).
    void switchTo(target).then(() => openNotificationLink(url));
    return;
  }
  // Each opens its page on home, so Back leads home (SPEC §3.8).
  switch (link.section) {
    case "calendar": {
      const id = link.params.id;
      if (!id) return openPage({ name: "calendar" });
      return openPage({ name: "calendar" }, { name: "item", id }, ...(link.params.run ? [{ name: "run" as const, itemId: id, runId: link.params.run }] : []));
    }
    case "live":
      goHome();
      pendingAnswer = liveAnswerOf(link);
      sendLiveAnswer(currentTransport());
      return;
    case "brain":
      // A Brain notification names its thread: a past one opens read-only.
      if (link.params.thread) openBrainThread(link.params.thread);
      if (link.path[0] === "work" && link.path[1]) return openPage({ name: "workDetail", workId: link.path[1] });
      return goHome();
    case "sessions":
      // mewla://sessions/<id> opens that Session of the current computer; the inbox push opens the tray.
      if (link.path[0]) return openPage({ name: "session", workerId: link.path[0] });
      return goHome({ tray: true });
    case "friends": {
      // Friends' own pushes: its drafts or its notifications.
      const list = link.path[0] === "drafts" ? "friends-drafts" : link.path[0] === "notifications" ? "friends-notifications" : null;
      return openPage({ name: "computer" }, { name: "friends" }, ...(list ? [{ name: list } as const] : []));
    }
  }
}

export function startNotifications(): void {
  workLinks.openSession = (sessionId: string) => openNotificationLink(`mewla://sessions/${encodeURIComponent(sessionId)}`);
  workLinks.openCalendarItem = (itemId, runId) => openNotificationLink(`mewla://calendar?id=${encodeURIComponent(itemId)}${runId ? `&run=${encodeURIComponent(runId)}` : ""}`);
  workLinks.askBrain = (text) => {
    draftForBrain(text);
    goHome();
  };
  startCalendarAlerts();
  // Calendar's Enable granted notifications: register this phone now.
  reminderLinks.granted = () => {
    token = null;
    const transport = currentTransport();
    const server = current();
    if (transport && server) void register(transport, server);
  };
  effect(() => {
    if (!ready() || waitingLink === null) return;
    const url = waitingLink;
    waitingLink = null;
    openNotificationLink(url);
  });
  if (!hosted) return;

  void loadStrongAlerts();
  startReminderBanner();
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
  // Another computer: the calendar editor of the last one closes (the stack returns home in the shell).
  let lastServer: string | null = null;
  effect(() => {
    const id = current()?.id ?? null;
    if (lastServer && id !== lastServer) closeEditor();
    if (id) lastServer = id;
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
}
