import { cleanup, effect, host, Image, openUrl, platform, signal, Storage, setColorScheme, untracked, View, lifecycle } from "nsh";
import { getIPhoneStatus, iphoneOffered, iphoneStateWord, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { loadStrongAlerts, setStrongAlerts, strongAlerts } from "../../calendar/state";
import manifest from "../../nsh.json";
import { phoneAvailable, phoneLabsWord } from "../../phone";
import { daemon, daemonState } from "../daemon";
import { Page, Row, Section, StatusMark, SwitchRow } from "../../kit/kit";
import { followFriends, friendsEnabled } from "../friends/state";
import { pet } from "../pet/pets";
import { followVirtualScreen, virtualScreenState } from "../screen/VirtualScreen";
import type { PageNav } from "../routes";
import { current } from "../../session";
import { AboutUpdateRow } from "../../update/AboutUpdateRow";
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
 * offer: Labs keeps only Virtual screen there, and the Calendar section goes (as v0.3.1).
 */
const onWeb = () => platform.os === "web";

/** iPhone use on the current computer; Labs shows the row only where it is offered (a Mac). */
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
    <Page title="Settings" testID="settings">
      <Section title="Channels">
        <Row
          testID="settings-telegram"
          icon="paper-plane"
          title="Telegram"
          subtitle={current() ? telegramRow().subtitle : "No current server"}
          disabled={!current()}
          trailing={current() && telegramRow().status ? <StatusMark status={telegramRow().status!} label={telegramRow().label} /> : undefined}
          accessibilityLabel={current() ? telegramRowLabel() : "Telegram, No current server"}
          accessibilityHint={current() ? "Open Telegram details and actions" : undefined}
          chevron={Boolean(current())}
          onPress={current() ? () => props.nav.push({ name: "settings-telegram" }) : undefined}
        />
      </Section>

      <Section title="Agents">
        <Row
          testID="settings-providers"
          icon="key"
          title="Model Providers"
          subtitle={current() ? undefined : "No current server"}
          accessibilityHint="Choose how Codex and Claude connect to models"
          chevron
          onPress={() => props.nav.push({ name: "settings-providers" })}
        />
        <Row
          testID="settings-outside-agents"
          icon="brain"
          title="Outside agents"
          subtitle={current() ? undefined : "No current server"}
          disabled={!current()}
          accessibilityHint={current() ? "Approve Claude Code or Codex handing work to Brain" : undefined}
          chevron
          onPress={current() ? () => props.nav.push({ name: "settings-agents" }) : undefined}
        />
      </Section>

      <Section title="Labs">
        {daemon() && !virtualScreenState().unsupported ? <Row testID="settings-virtual-screen" icon="desktop" title="Virtual screen" value={virtualScreenState().enabled ? "On" : "Off"} chevron onPress={() => props.nav.push({ name: "settings-virtual-screen" })} /> : null}
        {onWeb() ? null : <Row testID="settings-friends" icon="people" title="Friends" value={friendsEnabled() ? "On" : "Off"} chevron onPress={() => props.nav.push({ name: "settings-friends" })} />}
        {!onWeb() && phoneAvailable() ? <Row testID="settings-phone" icon="phone" title="Phone use" value={phoneLabsWord()} chevron onPress={() => props.nav.push({ name: "settings-phone" })} /> : null}
        {!onWeb() && iphoneOffered(iphone()) ? <Row testID="settings-iphone" icon="phone" title="iPhone use" value={iphoneStateWord(iphone())} chevron onPress={() => props.nav.push({ name: "settings-iphone" })} /> : null}
      </Section>

      {onWeb() ? null : (
        <Section title="Calendar">
          <SwitchRow testID="strong-alerts" title="Strong calendar alerts" checked={strongAlerts()} onChange={(on) => void setStrongAlerts(on)} />
          {platform.os === "android" && strongAlerts() && fullScreenAllowed() === false ? (
            <Row
              testID="full-screen-alerts"
              title="Allow full-screen alerts"
              subtitle="Android needs your OK to show an alert over the lock screen"
              chevron
              onPress={() => void host.call("calendar.alertsOpenSettings").catch(() => {})}
            />
          ) : null}
        </Section>
      )}

      <Section title="Appearance">
        {(["system", "light", "dark"] as const).map((choice) => (
          <Row
            testID={`appearance-${choice}`}
            icon={choice === "system" ? "contrast" : choice === "light" ? "sun" : "moon"}
            title={choice === "system" ? "Auto" : choice === "light" ? "Light" : "Dark"}
            check={appearance() === choice}
            role="radio"
            selected={appearance() === choice}
            onPress={() => chooseAppearance(choice)}
          />
        ))}
      </Section>

      <Section title="Pet">
        <Row testID="settings-pet" leading={<Image src={pet().portrait} style={{ width: 22, height: 22 }} />} title="Pet" value={pet().name} chevron onPress={() => props.nav.push({ name: "settings-pet" })} />
      </Section>

      <Section title="About">
        <Row title="Mewla" value={`Version ${manifest.version}`} accessibilityLabel={`Mewla, Version ${manifest.version}`} />
        <AboutUpdateRow />
        <Row icon="shield-check" title="Privacy Policy" chevron onPress={() => void openUrl(PRIVACY_URL)} />
        <Row icon="help" title="Support" chevron onPress={() => void openUrl(SUPPORT_URL)} />
        <Row testID="settings-licenses" icon="book" title="Open-source Licenses" chevron onPress={() => props.nav.push({ name: "settings-licenses" })} />
      </Section>
      <View class="h-6" />
    </Page>
  );
}
