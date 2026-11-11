import { effect, host, lifecycle, mount, platform, Storage, View } from "nsh";
import { copyLegacyStorage } from "../core/legacyStorage";
import { installPlatform } from "../core/platform";
import { nshPlatform } from "./platform";
import { pairFromText } from "./screens/Onboarding";
import { current, loadSession, ready, resumeConnection } from "./session";
import { FirstRun, sectionOfLink, selectedTab, Shell } from "./shell/Shell";
import { readPairingLink } from "../core/pairFlow";
import { startNotifications } from "./push/push";

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
  // A browser opened with `mewla pair`'s link (`<origin>/#pair=<link>`): pair with it, once.
  if (platform.os === "web" && host.available()) {
    const link = (await host.call("links.initial").catch(() => null)) as string | null;
    if (link) void pairFromText(link);
  }
}
void start();
startNotifications();

// Coming back to the app skips the reconnect backoff.
effect(() => {
  if (lifecycle() === "active") resumeConnection();
});

// Links the system opens in the app: a pairing link pairs, like a scan; the others open a section.
host.on("links.open", (payload) => {
  const url = (payload as { url?: string } | null)?.url;
  if (!url) return;
  if (readPairingLink(url)) return void pairFromText(url);
  const section = sectionOfLink(url);
  if (section !== null) selectedTab.set(section);
});

mount(() => (
  <View class="flex-1 bg-background">
    {!ready() ? null : current() ? <Shell /> : <FirstRun />}
  </View>
));
