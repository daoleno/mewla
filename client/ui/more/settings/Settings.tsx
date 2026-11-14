import { cleanup, effect, host, openUrl, platform, signal, Storage, setColorScheme, untracked, View, lifecycle } from "nsh";
import { getIPhoneStatus, iphoneOffered, iphoneStateWord, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { loadStrongAlerts, setStrongAlerts, strongAlerts } from "../../calendar/state";
import manifest from "../../nsh.json";
import { phoneAvailable, phoneStateWord } from "../../phone";
import { daemon, daemonState } from "../daemon";
import { Page, Row, RowButton, Section, SwitchRow } from "../../kit/kit";
import { StateLine, tile } from "../../kit/RowTile";
import { followFriends, friendsEnabled } from "../friends/state";
import { pet } from "../../pets/pets";
import { followVirtualScreen, virtualScreenState } from "../screen/VirtualScreen";
import type { PageNav } from "../routes";
import { current } from "../../session";
import { AboutUpdateRow } from "../../update/AboutUpdateRow";
import { markAppUpdateSeen } from "../../update/hint";
import { appUpdateWaiting } from "../../update/update";
import { loadTelegram, telegramRow, telegramRowLabel } from "./telegramState";

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
    appearance.set(appearanceOf(stored));
    setColorScheme(appearanceOf(stored));
  }).catch(() => {});
}

function chooseAppearance(next: Appearance): void {
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
  // Telegram is read again whenever Settings is shown again: back from a page above it, or the app back in front.
  const depth = untracked(() => {
    const routes = props.nav.routes?.() ?? [];
    const at = routes.findIndex((route) => route.name === "settings");
    return at >= 0 ? at + 1 : routes.length;
  });
  let shownAs = "";
  effect(() => {
    const top = (props.nav.routes?.().length ?? depth) === depth;
    const key = `${top && lifecycle() === "active"}|${current()?.id ?? ""}`;
    untracked(() => {
      if (key === shownAs) return;
      shownAs = key;
      if (!key.startsWith("true|")) return;
      loadTelegram();
    });
  });
  // Shown with this app's update row on top: the cat's dot for that version goes.
  effect(() => {
    const top = (props.nav.routes?.().length ?? depth) === depth;
    if (top && lifecycle() === "active" && appUpdateWaiting()) untracked(markAppUpdateSeen);
  });
  followFriends();
  if (!onWeb()) void loadStrongAlerts();
  followVirtualScreen();
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
  effect(() => {
    if (daemon() && daemonState() === "connected") untracked(loadTelegram);
  });
  return (
    <Page display title="Settings" testID="settings">
      {/* While this app's update waits, its row comes first (About is at the page's end); then it is About's again. */}
      <If when={appUpdateWaiting()}><Section testID="settings-update"><AboutUpdateRow /></Section></If>
      <Section title="Channels">
        <Row
          testID="settings-telegram"
          lead={{ kind: "mark", mark: "telegram" }}
          title="Telegram"
          subtitle={current() ? telegramRow().subtitle : "No current server"}
          disabled={!current()}
          // While the first read runs the value stays empty: an endless spinner here kept the app from ever going quiet.
          trailing={<If when={current()}><If when={!telegramRow().loading}><If when={telegramRow().connect} else={<If when={telegramRow().tone && telegramRow().label}><StateLine testID="settings-telegram-state" tone={telegramRow().tone!} label={telegramRow().label!} /></If>}><RowButton testID="settings-telegram-connect" label="Connect" accessibilityLabel="Connect Telegram" onPress={() => props.nav.push({ name: "settings-telegram" })} /></If></If></If>}
          accessibilityLabel={current() ? telegramRowLabel() : "Telegram, No current server"}
          accessibilityHint={current() ? "Open Telegram details and actions" : undefined}
          chevron={Boolean(current()) && !telegramRow().connect}
          onPress={current() ? () => props.nav.push({ name: "settings-telegram" }) : undefined}
        />
      </Section>

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

      <Section title="About">
        <Row lead={tile("info")} title="Mewla" value={`Version ${manifest.version}`} accessibilityLabel={`Mewla, Version ${manifest.version}`} />
        <If when={!appUpdateWaiting()}><AboutUpdateRow /></If>
        <Row lead={tile("shield-check")} title="Privacy Policy" chevron onPress={() => void openUrl(PRIVACY_URL)} />
        <Row lead={tile("help")} title="Support" chevron onPress={() => void openUrl(SUPPORT_URL)} />
        <Row testID="settings-licenses" lead={tile("book")} title="Open-source Licenses" chevron onPress={() => props.nav.push({ name: "settings-licenses" })} />
      </Section>
      <View class="h-6" />
    </Page>
  );
}
