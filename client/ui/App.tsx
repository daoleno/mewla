import { effect, host, lifecycle, mount, View } from "nsh";
import { installPlatform } from "../core/platform";
import { nshPlatform } from "./platform";
import { pairFromText } from "./screens/Onboarding";
import { current, loadSession, ready, resumeConnection } from "./session";
import { FirstRun, sectionOfLink, selectedTab, Shell } from "./shell/Shell";
import { readPairingLink } from "../core/pairFlow";

installPlatform(nshPlatform());
void loadSession();

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
