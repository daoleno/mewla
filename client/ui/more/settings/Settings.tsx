import { cleanup, effect, host, openUrl, platform, signal, Storage, setColorScheme, untracked, View, lifecycle } from "nsh";
import { openWhatsNew } from "../../whatsnew/state";
import { getIPhoneStatus, iphoneOffered, iphoneStateWord, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { loadStrongAlerts, setStrongAlerts, strongAlerts } from "../../calendar/state";
import manifest from "../../nsh.json";
import { phoneAvailable, phoneStateWord } from "../../phone";
import { daemon } from "../daemon";
import { Page, Row, RowButton, Section, SwitchRow } from "../../kit/kit";
import { tile } from "../../kit/RowTile";
import { followFriends, friendsEnabled } from "../friends/state";
import { pet } from "../../pets/pets";
import { followVirtualScreen, virtualScreenState } from "../screen/VirtualScreen";
import type { PageNav } from "../routes";
import { current } from "../../session";
import { AboutUpdateRow } from "../../update/AboutUpdateRow";
import { appUpdateWaiting } from "../../update/update";
import { chooseUsage, followUsage, usageLockedText, usageState } from "./usage";

/** The appearance this phone chose: the system's, or always light or dark. */
export type Appearance = "system" | "light" | "dark";
const APPEARANCE_KEY = "mewla:theme_preference";
export const appearance = signal<Appearance>("system");

function appearanceOf(stored: string | null): Appearance {
  return stored === "classic-light" ? "light" : stored === "classic-dark" ? "dark" : "system";
}

/** Applies the stored appearance once at start (the Expo app's key and values). */
export function restoreAppearance(): void {
  void Storage.getItem(APPEARANCE_KEY).then((stored) => {
    // A Mac says in the app's log what it read and what it chose: its proof runs have no other window on it.
    if (platform.os === "macos") console.log(`appearance: read ${JSON.stringify(stored)}`);
    appearance.set(appearanceOf(stored));
    setColorScheme(appearanceOf(stored));
  }).catch(() => {});
}

export function chooseAppearance(next: Appearance): void {
  if (platform.os === "macos") console.log(`appearance: chose ${next}`);
  appearance.set(next);
  setColorScheme(next);
  void Storage.setItem(APPEARANCE_KEY, next === "system" ? "system" : `classic-${next}`).catch(() => {});
}

/** Whether Android lets the calendar alert show over the lock screen (null: not asked, or not Android). */
const fullScreenAllowed = signal<boolean | null>(null);

function readAlertCapabilities(): void {
  if (platform.os !== "android" || !host.available()) return;
  void host
    .call<{ fullScreen?: boolean }>("calendar.alertsCapabilities")
    .then((caps) => fullScreenAllowed.set(caps?.fullScreen !== false))
    .catch(() => fullScreenAllowed.set(null));
}

/**
 * A browser is served by its computer and has no phone, alerts or iPhone of its own to
 * offer: Phone use, Labs and the Calendar section go there (as v0.3.1).
 */
const onWeb = () => platform.os === "web";

/** iPhone use on the current computer; its row shows only where it is offered (a Mac). */
const iphone = signal<IPhoneDaemonStatus | null>(null);
let iphoneFor = "";

function readIPhone(): void {
  const server = daemon()?.server;
  if (!server || onWeb()) return;
  void getIPhoneStatus(server)
    .then((next) => daemon()?.server.id === server.id && iphone.set(next))
    .catch(() => daemon()?.server.id === server.id && iphone.set(null));
}

export const PRIVACY_URL = "https://mewla.ai/privacy/";
export const SUPPORT_URL = "https://mewla.ai/support/";

/**
 * Settings: what this phone decides and what this app connects to, then About. The computers
 * (which one is in use, pairing, each one's details) are on the cat's computer page.
 */
export function Settings(props: { nav: PageNav }) {
  followFriends();
  if (!onWeb()) void loadStrongAlerts();
  followVirtualScreen();
  followUsage();
  // iPhone use, every 15 s while Settings is open: an agent may start or stop using it.
  let iphoneTimer: ReturnType<typeof setInterval> | null = null;
  effect(() => {
    const id = daemon()?.server.id ?? "";
    if (iphoneTimer) clearInterval(iphoneTimer);
    iphoneTimer = null;
    const active = lifecycle() === "active";
    untracked(() => {
      if (id !== iphoneFor) {
        iphoneFor = id;
        iphone.set(null);
      }
    });
    if (!id || !active || onWeb()) return;
    untracked(readIPhone);
    iphoneTimer = setInterval(readIPhone, 15_000);
  });
  cleanup(() => iphoneTimer && clearInterval(iphoneTimer));
  // Back from Android's settings: ask again whether the lock-screen alert is allowed.
  effect(() => {
    if (lifecycle() === "active") untracked(readAlertCapabilities);
  });
  return (
    <Page display title="Settings" testID="settings">
      {/* While this app's update waits, its row comes first (About is at the page's end); then it is About's again. */}
      <If when={appUpdateWaiting()}><Section testID="settings-update"><AboutUpdateRow /></Section></If>
      <Section title="Agents">
        <Row
          testID="settings-providers"
          lead={tile("key")}
          title="Model Providers"
          subtitle={current() ? undefined : "No current server"}
          accessibilityHint="Choose how Codex and Claude connect to models"
          chevron
          onPress={() => props.nav.push({ name: "settings-providers" })}
        />
        <Row
          testID="settings-outside-agents"
          lead={tile("brain")}
          title="Outside agents"
          subtitle={current() ? undefined : "No current server"}
          disabled={!current()}
          accessibilityHint={current() ? "Approve Claude Code or Codex handing work to Brain" : undefined}
          chevron
          onPress={current() ? () => props.nav.push({ name: "settings-agents" }) : undefined}
        />
        <If when={daemon() && !virtualScreenState().unsupported}><Row testID="settings-virtual-screen" lead={tile("desktop")} title="Virtual screen" value={virtualScreenState().enabled ? "On" : virtualScreenState().enabled === false ? "Off" : undefined} chevron onPress={() => props.nav.push({ name: "settings-virtual-screen" })} /></If>
        <If when={!onWeb() && phoneAvailable()}><Row testID="settings-phone" lead={tile("phone")} title="Phone use" value={phoneStateWord()} chevron onPress={() => props.nav.push({ name: "settings-phone" })} /></If>
      </Section>

      {/* Labs: Friends and iPhone use, still trials. A browser offers neither, so there the section goes. */}
      <If when={!onWeb()}><Section title="Labs">
        <Row testID="settings-friends" lead={tile("people")} title="Friends" value={friendsEnabled() ? "On" : "Off"} chevron onPress={() => props.nav.push({ name: "settings-friends" })} />
        <If when={iphoneOffered(iphone())}><Row testID="settings-iphone" lead={tile("phone")} title="iPhone use" value={iphoneStateWord(iphone())} chevron onPress={() => props.nav.push({ name: "settings-iphone" })} /></If>
      </Section></If>

      <If when={!onWeb()}><Section title="Calendar">
          <SwitchRow testID="strong-alerts" lead={tile("bell")} title="Strong calendar alerts" checked={strongAlerts()} onChange={(on) => void setStrongAlerts(on)} />
          <If when={platform.os === "android" && strongAlerts() && fullScreenAllowed() === false}><Row
              testID="full-screen-alerts"
              lead={tile("lock")}
              title="Allow full-screen alerts"
              chevron
              onPress={() => void host.call("calendar.alertsOpenSettings").catch(() => {})}
            /></If>
        </Section></If>

      <Section title="Appearance">
        {(["system", "light", "dark"] as const).map((choice) => (
          <Row
            testID={`appearance-${choice}`}
            lead={tile(choice === "system" ? "contrast" : choice === "light" ? "sun" : "moon")}
            title={choice === "system" ? "Auto" : choice === "light" ? "Light" : "Dark"}
            check={appearance() === choice}
            role="radio"
            selected={appearance() === choice}
            onPress={() => chooseAppearance(choice)}
          />
        ))}
      </Section>

      <Section title="Pet">
        <Row testID="settings-pet" lead={{ kind: "art", src: pet().art.portrait }} title="Pet" value={pet().name} chevron onPress={() => props.nav.push({ name: "settings-pet" })} />
      </Section>

      {/* Usage counts: the current computer holds the switch, for itself and its phones; an older Mewla has none. */}
      <If when={usageState().enabled !== null}><Section title="Privacy">
        <SwitchRow
          testID="usage-counts"
          lead={tile("stats")}
          title="Share anonymous usage counts"
          subtitle={usageLockedText(usageState().locked) || usageState().error || undefined}
          checked={usageState().enabled === true}
          busy={usageState().busy}
          disabled={usageState().locked !== ""}
          onChange={chooseUsage}
        />
      </Section></If>

      <Section title="About">
        <Row lead={tile("info")} title="Mewla" value={`Version ${manifest.version}`} accessibilityLabel={`Mewla, Version ${manifest.version}`} />
        <Row testID="settings-whats-new" lead={tile("bulb")} title="What's new" chevron onPress={() => openWhatsNew()} />
        <If when={!appUpdateWaiting()}><AboutUpdateRow /></If>
        <Row lead={tile("shield-check")} title="Privacy Policy" chevron onPress={() => void openUrl(PRIVACY_URL)} />
        <Row lead={tile("help")} title="Support" chevron onPress={() => void openUrl(SUPPORT_URL)} />
        <Row testID="settings-licenses" lead={tile("book")} title="Open-source Licenses" chevron onPress={() => props.nav.push({ name: "settings-licenses" })} />
      </Section>
      <View class="h-6" />
    </Page>
  );
}
