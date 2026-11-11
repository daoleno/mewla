import { effect, host, mount, platform, Storage, View } from "nsh";
import { lifecycle } from "./lifecycle";
import { copyLegacyStorage } from "../core/legacyStorage";
import { installPlatform } from "../core/platform";
import { nshPlatform } from "./platform";
import { justPaired, pairFromText } from "./pairing";
import { CrashReportSheet, loadCrashReports } from "./crash/CrashReports";
import { current, loadSession, ready, resumeConnection } from "./session";
import { FirstRun, openPhonePage, openSettingsLink, sectionOfLink, selectedTab, Shell } from "./shell/Shell";
import { isPhoneLink } from "./phone";
import { readPairingLink } from "../core/pairFlow";
import { startNotifications } from "./push/push";
import { startAppUpdate } from "./update/update";

installPlatform(nshPlatform());

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
  await loadSession();
  void loadCrashReports();
  // A browser opened with `mewla pair`'s link (`<origin>/#pair=<link>`): pair with it, once.
  if (platform.os === "web" && host.available()) {
    const link = (await host.call("links.initial").catch(() => null)) as string | null;
    if (link) void pairFromText(link);
  }
}
void start();
startNotifications();
startAppUpdate();

// Coming back to the app skips the reconnect backoff.
effect(() => {
  if (lifecycle() === "active") resumeConnection();
});

// Links the system opens in the app: a pairing link pairs, like a scan; the others open a section.
host.on("links.open", (payload) => {
  const url = (payload as { url?: string } | null)?.url;
  if (!url) return;
  if (readPairingLink(url)) return void pairFromText(url);
  if (isPhoneLink(url)) return void openPhonePage();
  if (openSettingsLink(url)) return;
  const section = sectionOfLink(url);
  if (section !== null) selectedTab.set(section);
});

mount(() => (
  <View class="flex-1 bg-background">
    {/* The app is not under the connected step: on Android its native tab and navigation views were drawn
        over the step once the notification prompt or a navigation came back, so the step vanished. */}
    {!ready() ? null : current() && !justPaired() ? <Shell /> : null}
    {/* First run, and on a phone the connected step after any pairing. */}
    {ready() && (!current() || justPaired()) ? <View class="absolute inset-0 bg-background"><FirstRun /></View> : null}
    <CrashReportSheet />
  </View>
));
