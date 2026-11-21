import { cleanup, colorScheme, effect, host, lifecycle, mount, platform, safeArea, Storage, Text, View } from "nsh";
import { openedForSomething } from "./whatsnew/state";
import { installPlatform } from "../core/platform";
import { keepReadingPlaces, loadReadingPlaces } from "../core/timeline/readingPlace";
import { nshPlatform } from "./platform";
import { justPaired, linkPairing, pairFromText, pairingStatus } from "./pairing";
import { CrashReportSheet, loadCrashReports } from "./crash/CrashReports";
import { brainDraft, connection, current, loadSession, networkChanged, openBrainThread, ready, resumeConnection, setConnectionAway } from "./session";
import { FirstRun, openPhonePage, openSettingsLink, Shell } from "./shell/Shell";
import { isPhoneLink } from "./phone";
import { readPairingLink } from "../core/pairFlow";
import { openNotificationLink, startNotifications } from "./push/push";
import { startShareIn } from "./more/files/shareIn";
import { goHome } from "./shell/nav";
import { tray } from "./tray/state";
import { startAppUpdate } from "./update/update";
import { takeHandover } from "./update/moveIn";
import { startMac } from "./mac/mac";
// Registers the monospaced face theme.json's `font-mono` names, before the first frame.
import "./kit/monoFont";

async function start(): Promise<void> {
  // Where chats were read to before this start (a reload while reading), before any chat opens.
  await loadReadingPlaces();
  // The new app of a move (11-21) takes what the old one staged before it reads its computers.
  await takeHandover();
  await loadSession();
  void loadCrashReports();
  // A browser opened with `mewla pair`'s link (`<origin>/#pair=<link>`): pair with it, once.
  if (platform.os === "web" && host.available()) {
    const link = (await host.call("links.initial").catch(() => null)) as string | null;
    if (link) void pairFromText(link);
  }
}

// A hot patch (the dev channel) runs this module again. Starting again closed the live connection and Brain's chat
// (its draft; the thread came back at its newest row) to open them anew, and registered notifications, app update
// and the listeners below once more. The flag is on globalThis, which a re-run keeps.
const processState = globalThis as { __mewlaStarted?: boolean };

/** The app's start: its platform, its computer and connection, notifications, app update, links. Runs once per process. */
export function startApp(): void {
  if (processState.__mewlaStarted) return;
  processState.__mewlaStarted = true;
  installPlatform(nshPlatform());
  void start();
  startNotifications();
  startAppUpdate();
  startMac();

  // Android's navigation bar draws its buttons for the app's appearance, also when it changes while the app runs
  // (Settings › Appearance, or the system's when it follows it); NSH sets only the status bar. iOS has no such bar.
  effect(() => {
    const dark = colorScheme() === "dark";
    if (platform.os === "android" && host.available()) void host.call("app.navigationBar", { dark }).catch(() => undefined);
  });

  // The Android host reopens the app when these beats stop for 10 s while it is in front (JsWatchdog.kt): NSH's
  // engine can stall (0.5.0 spun forever, longbridge/nsh#1984, fixed in 0.5.1), and a reopened app beats a frozen
  // one. iOS has no watchdog yet.
  if (platform.os === "android" && host.available()) setInterval(() => void host.call("app.alive").catch(() => undefined), 2_000);

  // Coming back to the app skips the reconnect backoff.
  // Leaving it keeps where chats were read to: the system may make the app anew before it comes back.
  // A Mac never freezes the app, and its menu bar cat, perch and windows stay in sight while another app is in front
  // (NSH's `inactive`) or none of its windows shows: the computer's news never waits there.
  effect(() => {
    const active = lifecycle() === "active";
    setConnectionAway(!active && platform.os !== "macos");
    if (active) resumeConnection();
    else keepReadingPlaces();
  });

  // The phone's network changed (the Android and iOS hosts' `link.network`): a reconnect waiting out its backoff goes
  // now, and a live socket proves it still works on the new network instead of going silent for 25 s.
  host.on("link.network", (payload) => networkChanged((payload as { online?: unknown } | null)?.online !== false));

  // What other apps share or open with Mewla (Android): files and text for Brain's composer, from the app's start on.
  startShareIn(
    { paired: () => ready() && !!current(), draft: () => !!brainDraft(), pairAgain: () => tray().state.kind === "pair", connected: () => connection() === "connected" },
    // The person came for this: Brain's current chat, whatever was open.
    () => {
      openedForSomething();
      openBrainThread(null);
      goHome();
    },
  );

  // Links the system opens in the app: a pairing link pairs, like a scan; the others open their page.
  host.on("links.open", (payload) => {
    const url = (payload as { url?: string } | null)?.url;
    if (!url) return;
    openedForSomething();
    if (readPairingLink(url)) return void pairFromText(url);
    if (isPhoneLink(url)) return void openPhonePage();
    if (openSettingsLink(url)) return;
    // Brain, a Session, the Calendar, a live answer, Friends: what a notification, an alert or a widget sends.
    openNotificationLink(url);
  });
}
startApp();

/** A pairing link opened while the app runs: what it is doing, then why it failed (tap or 8 s to dismiss). */
function LinkPairingNote() {
  effect(() => {
    if (!linkPairing()?.problem) return;
    const timer = setTimeout(() => linkPairing.set(null), 8_000);
    cleanup(() => clearTimeout(timer));
  });
  return (
    <View class="absolute" style={{ top: safeArea().top + 8, left: safeArea().left + 12, right: safeArea().right + 12 }}>
      <If when={linkPairing()}><View testID="link-pairing" onPress={() => linkPairing()?.problem && linkPairing.set(null)} class="gap-0.5 rounded-(--radius-group) bg-card px-4 py-3">
          <Text class="text-body font-semibold">
            {linkPairing()!.problem?.title ?? (pairingStatus() || "Pairing…")}
          </Text>
          <If when={linkPairing()!.problem}><Text class="text-sub text-muted-foreground">{linkPairing()!.problem!.detail}</Text></If>
        </View></If>
    </View>
  );
}

mount(() => (
  // The ink every text takes unless it says its own (NSH draws a bare Text black, unreadable in Dark).
  <View class="flex-1 bg-background text-foreground">
    {/* The app is not under the connected step: on Android the step vanished behind the app while it still
        held (after the notification prompt, after a Settings pairing), most likely the app's native views. */}
    <If when={ready()}><If when={current() && !justPaired()}><Shell /></If></If>
    {/* First run, and on a phone the connected step after any pairing. */}
    <If when={ready() && (!current() || justPaired())}><View class="absolute inset-0 bg-background"><FirstRun /></View></If>
    <If when={ready() && current() && !justPaired()}><LinkPairingNote /></If>
    <CrashReportSheet />
  </View>
));
