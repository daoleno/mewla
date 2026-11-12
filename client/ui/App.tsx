import { cleanup, effect, host, lifecycle, mount, platform, safeArea, Storage, Text, View } from "nsh";
import { copyLegacyStorage } from "../core/legacyStorage";
import { installPlatform } from "../core/platform";
import { nshPlatform } from "./platform";
import { justPaired, linkPairing, pairFromText, pairingStatus } from "./pairing";
import { CrashReportSheet, loadCrashReports } from "./crash/CrashReports";
import { current, loadSession, ready, resumeConnection } from "./session";
import { FirstRun, openPhonePage, openPlace, openSettingsLink, sectionOfLink, selectedTab, Shell } from "./shell/Shell";
import { isPhoneLink } from "./phone";
import { readPairingLink } from "../core/pairFlow";
import { placeOfLink } from "../core/web/places";
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
  // `mewla://stats`, `…/screen`, `…/terminal/<id>`, `…/work/<id>`…: the page it names, as the Expo app's routes did (parity #48).
  const place = placeOfLink(url);
  if (place) return void openPlace(place);
  const section = sectionOfLink(url);
  if (section !== null) selectedTab.set(section);
});

/** A pairing link opened while the app runs: what it is doing, then why it failed (tap or 8 s to dismiss). */
function LinkPairingNote() {
  effect(() => {
    if (!linkPairing()?.problem) return;
    const timer = setTimeout(() => linkPairing.set(null), 8_000);
    cleanup(() => clearTimeout(timer));
  });
  return (
    <View class="absolute left-3 right-3" style={{ top: safeArea().top + 8 }}>
      {linkPairing() ? (
        <View testID="link-pairing" onPress={() => linkPairing()?.problem && linkPairing.set(null)} class="gap-0.5 rounded-xl border border-border bg-card px-3 py-2.5">
          <Text class={linkPairing()!.problem ? "text-sm font-semibold text-destructive" : "text-sm font-semibold"}>
            {linkPairing()!.problem?.title ?? (pairingStatus() || "Pairing…")}
          </Text>
          {linkPairing()!.problem ? <Text class="text-sm text-muted-foreground">{linkPairing()!.problem!.detail}</Text> : null}
        </View>
      ) : null}
    </View>
  );
}

mount(() => (
  // The ink every text takes unless it says its own (NSH draws a bare Text black, unreadable in Dark).
  <View class="flex-1 bg-background text-foreground">
    {/* The app is not under the connected step: on Android the step vanished behind the app while it still
        held (after the notification prompt, after a Settings pairing), most likely the app's native views. */}
    {!ready() ? null : current() && !justPaired() ? <Shell /> : null}
    {/* First run, and on a phone the connected step after any pairing. */}
    {ready() && (!current() || justPaired()) ? <View class="absolute inset-0 bg-background"><FirstRun /></View> : null}
    {ready() && current() && !justPaired() ? <LinkPairingNote /> : null}
    <CrashReportSheet />
  </View>
));
