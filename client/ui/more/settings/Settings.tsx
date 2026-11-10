import { cleanup, effect, host, Image, lifecycle, openUrl, platform, signal, Storage, setColorScheme, untracked, View } from "nsh";
import { getIPhoneStatus, iphoneOffered, iphoneStateWord, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { loadStrongAlerts, setStrongAlerts, strongAlerts } from "../../calendar/state";
import manifest from "../../nsh.json";
import { daemon, daemonState } from "../daemon";
import { Icon } from "../kit/Icon";
import { Page, Row, Section, StatusMark, SwitchRow } from "../kit/kit";
import { followFriends, friendsEnabled } from "../friends/state";
import { pet } from "../pet/pets";
import { followVirtualScreen, virtualScreenState } from "../screen/VirtualScreen";
import type { PageNav } from "../routes";
import { current, servers } from "../../session";
import { probeComputers, statusWord } from "./computers";
import { loadTelegram, telegramRow } from "./telegramState";

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

/** iPhone use on the current computer; Labs shows the row only where it is offered (a Mac). */
const iphone = signal<IPhoneDaemonStatus | null>(null);
let iphoneFor = "";

function readIPhone(): void {
  const server = daemon()?.server;
  if (!server) return;
  void getIPhoneStatus(server)
    .then((next) => daemon()?.server.id === server.id && iphone.set(next))
    .catch(() => daemon()?.server.id === server.id && iphone.set(null));
}

export const PRIVACY_URL = "https://mewla.ai/privacy/";
export const SUPPORT_URL = "https://mewla.ai/support/";

/** Settings: the computers, then each thing this phone decides, then About. */
export function Settings(props: { nav: PageNav; onPair: () => void }) {
  void untracked(probeComputers);
  followFriends();
  void loadStrongAlerts();
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
    if (!id || !active) return;
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
      <Section title="Computers">
        {servers().map((s) => (
          <Row
            testID={`computer-${s.id}`}
            leading={<Icon name="desktop" />}
            title={s.name || s.url}
            subtitle={statusWord(s) || undefined}
            value={s.id === current()?.id ? "Current" : undefined}
            chevron
            onPress={() => props.nav.push({ name: "settings-computer", id: s.id })}
          />
        ))}
        <Row testID="pair-another" icon="add" title="Pair a computer" chevron onPress={props.onPair} />
      </Section>

      <Section title="Channels">
        <Row
          testID="settings-telegram"
          icon="paper-plane"
          title="Telegram"
          subtitle={current() ? telegramRow().subtitle : "No current server"}
          disabled={!current()}
          trailing={current() && telegramRow().status ? <StatusMark status={telegramRow().status!} label={telegramRow().label} /> : undefined}
          chevron={Boolean(current())}
          onPress={current() ? () => props.nav.push({ name: "settings-telegram" }) : undefined}
        />
      </Section>

      <Section title="Agents">
        <Row testID="settings-providers" icon="key" title="Model Providers" subtitle={current() ? undefined : "No current server"} chevron onPress={() => props.nav.push({ name: "settings-providers" })} />
        <Row testID="settings-outside-agents" icon="brain" title="Outside agents" disabled={!current()} chevron onPress={current() ? () => props.nav.push({ name: "settings-agents" }) : undefined} />
      </Section>

      <Section title="Labs">
        {daemon() && !virtualScreenState().unsupported ? <Row testID="settings-virtual-screen" icon="desktop" title="Virtual screen" value={virtualScreenState().enabled ? "On" : "Off"} chevron onPress={() => props.nav.push({ name: "settings-virtual-screen" })} /> : null}
        <Row testID="settings-friends" icon="people" title="Friends" value={friendsEnabled() ? "On" : "Off"} chevron onPress={() => props.nav.push({ name: "settings-friends" })} />
        {platform.os === "android" ? <Row testID="settings-phone" icon="phone" title="Phone use" chevron onPress={() => props.nav.push({ name: "settings-phone" })} /> : null}
        {iphoneOffered(iphone()) ? <Row testID="settings-iphone" icon="phone" title="iPhone use" value={iphoneStateWord(iphone())} chevron onPress={() => props.nav.push({ name: "settings-iphone" })} /> : null}
      </Section>

      {platform.os === "web" ? null : (
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
            onPress={() => chooseAppearance(choice)}
          />
        ))}
      </Section>

      <Section title="Pet">
        <Row testID="settings-pet" leading={<Image src={pet().portrait} style={{ width: 22, height: 22 }} />} title="Pet" value={pet().name} chevron onPress={() => props.nav.push({ name: "settings-pet" })} />
      </Section>

      <Section title="About">
        <Row title="Mewla" value={`Version ${manifest.version}`} />
        <Row icon="shield-check" title="Privacy Policy" chevron onPress={() => void openUrl(PRIVACY_URL)} />
        <Row icon="help" title="Support" chevron onPress={() => void openUrl(SUPPORT_URL)} />
        <Row testID="settings-licenses" icon="book" title="Open-source Licenses" chevron onPress={() => props.nav.push({ name: "settings-licenses" })} />
      </Section>
      <View class="h-6" />
    </Page>
  );
}
