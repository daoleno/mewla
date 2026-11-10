import { effect, openUrl, signal, Storage, setColorScheme, untracked, View } from "nsh";
import manifest from "../../nsh.json";
import { daemon, daemonState } from "../daemon";
import { Icon } from "../kit/Icon";
import { Page, Row, Section, StatusMark } from "../kit/kit";
import type { PageNav } from "../routes";
import { current, servers } from "../../session";
import { probeComputers, statusWord } from "./computers";
import { loadTelegram, telegramRow } from "./telegram";

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

export const PRIVACY_URL = "https://mewla.ai/privacy/";
export const SUPPORT_URL = "https://mewla.ai/support/";

/** Settings: the computers, then each thing this phone decides, then About. */
export function Settings(props: { nav: PageNav; onPair: () => void }) {
  void untracked(probeComputers);
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
        <Row testID="settings-friends" icon="people" title="Friends" chevron onPress={() => props.nav.push({ name: "settings-friends" })} />
        <Row testID="settings-iphone" icon="phone" title="iPhone use" disabled={!current()} chevron onPress={current() ? () => props.nav.push({ name: "settings-iphone" }) : undefined} />
      </Section>

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
        <Row testID="settings-pet" icon="paw" title="Pet" chevron onPress={() => props.nav.push({ name: "settings-pet" })} />
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
