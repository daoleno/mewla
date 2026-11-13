import { cleanup, colorScheme, effect, host, lifecycle, mount, platform, safeArea, Storage, Text, View } from "nsh";
import { copyLegacyStorage } from "../core/legacyStorage";
import { installPlatform } from "../core/platform";
import { keepReadingPlaces, loadReadingPlaces } from "../core/timeline/readingPlace";
import { nshPlatform } from "./platform";
import { justPaired, linkPairing, pairFromText, pairingStatus } from "./pairing";
import { CrashReportSheet, loadCrashReports } from "./crash/CrashReports";
import { current, loadSession, ready, resumeConnection } from "./session";
import { FirstRun, openPhonePage, openSettingsLink, Shell } from "./shell/Shell";
import { isPhoneLink } from "./phone";
import { readPairingLink } from "../core/pairFlow";
import { openNotificationLink, startNotifications } from "./push/push";
import { startAppUpdate } from "./update/update";
// Registers the monospaced face theme.json's `font-mono` names, before the first frame.
import "./kit/monoFont";

// The first start after the in-place upgrade takes over what the Expo app kept (computers,
// drafts, preferences); the device key stays where it was, in the host's secure store.
async function start(): Promise<void> {
  if (host.available()) {
    await copyLegacyStorage(
      {
        entries: async () => (await host.call("app.legacyStorage")) as Record<string, string> | null,
        copied: async () => void (await host.call("app.legacyStorageCopied")),
      },
      Storage,
    ).catch((error) => console.warn("legacy storage not copied:", error));
  }
  // Where chats were read to before this start (a reload while reading), before any chat opens.
  await loadReadingPlaces();
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

  // Android's navigation bar draws its buttons for the app's appearance, also when it changes while the app runs
  // (Settings › Appearance, or the system's when it follows it); NSH sets only the status bar. iOS has no such bar.
  effect(() => {
    const dark = colorScheme() === "dark";
    if (platform.os === "android" && host.available()) void host.call("app.navigationBar", { dark }).catch(() => undefined);
  });

  // Coming back to the app skips the reconnect backoff.
  // Leaving it keeps where chats were read to: the system may make the app anew before it comes back.
  effect(() => {
    if (lifecycle() === "active") resumeConnection();
    else keepReadingPlaces();
  });

  // Links the system opens in the app: a pairing link pairs, like a scan; the others open their page.
  host.on("links.open", (payload) => {
    const url = (payload as { url?: string } | null)?.url;
    if (!url) return;
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
    <View class="absolute left-3 right-3" style={{ top: safeArea().top + 8 }}>
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
