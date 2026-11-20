import { cleanup, effect, keymap, Scroll, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { Window } from "nsh/desktop";
import { getAgentUse, changeAgentUse, type AgentUseStatus } from "../../core/settings/computer";
import { screenRequest, type ScreenPermission } from "../../core/screen/screen";
import { BrainHostPanel } from "../brain/BrainHostSheet";
import { Icon, type IconName } from "../kit/Icon";
import { Row, RowButton, Section, SwitchRow } from "../kit/kit";
import { StateLine } from "../kit/RowTile";
import { Segmented } from "../kit/Segmented";
import manifest from "../nsh.json";
import { appearance, chooseAppearance, type Appearance } from "../more/settings/Settings";
import { loadTelegram, telegramRow } from "../more/settings/telegramState";
import type { MorePage } from "../more/routes";
import { pet } from "../pets/pets";
import { connection, current } from "../session";
import { runDesktopCommand } from "../shell/desktop/Overlays";
import { pageNav } from "../shell/nav";
import { atLogin, readStartAtLogin, setStartAtLogin } from "./login";
import { ShortcutRecorder } from "./QuickChatShortcut";
import { daemonPairing, isThisMac, MAC_TITLE_BAR, showMainWindow, type DaemonPairing } from "./local";
import { MaterialTint } from "./materials";

/**
 * Settings (⌘,; DESIGN-MAC.md §2.5, `m-settings`, `m-access`): a window of its own, 720 × 560, with a 190 sidebar in
 * the sidebar material (rows 28 tall at 13, 17 icons) and a page per decision. Paired devices live on This Mac.
 */
export const MAC_SETTINGS = "mewla-settings";
const SIZE = { width: 720, height: 560 };
const SIDEBAR = 190;

export type MacSettingsPage = "general" | "connections" | "agents" | "access" | "pet";
const PAGES: ReadonlyArray<{ key: MacSettingsPage; label: string; icon: IconName }> = [
  { key: "general", label: "General", icon: "settings" },
  { key: "connections", label: "Connections", icon: "link" },
  { key: "agents", label: "Agents", icon: "brain" },
  { key: "access", label: "Privacy & access", icon: "shield-check" },
  { key: "pet", label: "Pet", icon: "paw" },
];

export const macSettingsPage = signal<MacSettingsPage>("general");

/** The perch's shortcut as the system gave it (mac.ts registers it), or null while another app holds it. */
export const macShortcut = signal<string | null>(null);

/** Settings… (the cat's menu, the Mewla menu, ⌘,, This Mac's Settings row): the window, in front. */
export function openMacSettings(page?: MacSettingsPage): void {
  if (page) macSettingsPage.set(page);
  Window.open(MacSettings, {
    id: MAC_SETTINGS,
    kind: "normal",
    title: "Settings",
    defaultSize: SIZE,
    minSize: SIZE,
    resizable: false,
    maximizable: false,
    titleBar: MAC_TITLE_BAR,
    transparent: true,
  }).focus();
}

/** A page that lives in the window's own stack (Telegram, the model providers, the pet): the window shows it there. */
function openInWindow(route: MorePage): void {
  showMainWindow();
  runDesktopCommand({ kind: "go", key: "settings" });
  pageNav.push(route);
}

function SidebarRow(props: { page: (typeof PAGES)[number] }) {
  const chosen = () => macSettingsPage() === props.page.key;
  return (
    <Button testID={`mac-settings-${props.page.key}`} role="tab" accessibilityLabel={props.page.label} selected={chosen()} onPress={() => macSettingsPage.set(props.page.key)}
      class={`flex-row items-center gap-2 rounded-md px-2 ${chosen() ? "bg-accent" : "hover:bg-accent"}`} style={{ height: 28 }}>
      <Icon name={props.page.icon} size={17} tone={chosen() ? undefined : "soft"} />
      <Text class={chosen() ? "font-semibold" : ""} style={{ fontSize: 13 }}>{props.page.label}</Text>
    </Button>
  );
}

function Heading(props: { title: string }) {
  return <Text role="header" class="font-bold" style={{ fontSize: 17, marginBottom: 4 }}>{props.title}</Text>;
}

function General(props: { shortcut: () => string | null }) {
  readStartAtLogin();
  // A click on the key cap records a new one (§2.5).
  const recording = signal(false);
  return (
    <View>
      <Heading title="General" />
      <Section>
        <SwitchRow testID="mac-settings-login" title="Start at login" checked={atLogin()} onChange={(on) => void setStartAtLogin(on)} />
        <Row title="Quick chat" accessibilityLabel={props.shortcut() ? `Quick chat, ${keymap.label(props.shortcut()!)}` : "Quick chat, held by another app"}
          trailing={<ShortcutRecorder compact testID="mac-settings-shortcut" recording={recording} onRecording={(on) => recording.set(on)} />} />
      </Section>
      <Section>
        <Row title="Appearance" trailing={<View style={{ width: 230 }}><Segmented<Appearance> testID="mac-settings-appearance" accessibilityLabel="Appearance"
          options={[{ key: "system", label: "System" }, { key: "light", label: "Light" }, { key: "dark", label: "Dark" }]} value={appearance()} onChange={chooseAppearance} /></View>} />
      </Section>
      <Section>
        <Row testID="mac-settings-version" title={`Mewla ${manifest.version}`} />
      </Section>
    </View>
  );
}

function Connections() {
  const pairing = signal<DaemonPairing | null>(null);
  effect(() => {
    if (connection() !== "connected" || !isThisMac(current())) return;
    untracked(() => {
      loadTelegram();
      void daemonPairing().then((next) => pairing.set(next)).catch(() => pairing.set(null));
    });
  });
  const host = (address?: string) => (address ? address.replace(/^[a-z]+:\/\//, "").replace(/\/$/, "") : "");
  return (
    <View>
      <Heading title="Connections" />
      <ThisMacOnly />
      <If when={isThisMac(current())}><>
      <Section title="Phones reach this Mac">
        <Row testID="mac-settings-remote" title="Anywhere" value={host(pairing()?.phone_address) || "Off"} />
        <Row testID="mac-settings-wifi" title="This Wi‑Fi" value={host(pairing()?.wifi_address) || "Not on Wi‑Fi"} />
      </Section>
      <Section title="Channels">
        <Row testID="mac-settings-telegram" lead={{ kind: "mark", mark: "telegram" }} title="Telegram"
          trailing={<If when={telegramRow().tone && telegramRow().label}><StateLine tone={telegramRow().tone!} label={telegramRow().label!} /></If>}
          chevron onPress={() => openInWindow({ name: "settings-telegram" })} />
      </Section>
      </></If>
    </View>
  );
}

/** Another computer is the current one: these pages are this Mac's, so they say whose they are not. */
function ThisMacOnly() {
  return (
    <If when={current() && !isThisMac(current())}>
      <Text testID="mac-settings-other" class="text-sub text-muted-foreground" style={{ marginTop: 8 }}>{`${current()!.name} is in use, not this Mac.`}</Text>
    </If>
  );
}

function Agents() {
  return (
    <View>
      <Heading title="Agents" />
      <Section title="Brain's host" label={<View class="mb-2 mt-6 px-4"><Text class="text-caption font-semibold text-muted-foreground">Brain's host</Text></View>}>
        <View class="-mx-4"><BrainHostPanel onClose={() => {}} /></View>
      </Section>
      <Section>
        <Row testID="mac-settings-providers" title="Model providers" chevron onPress={() => openInWindow({ name: "settings-providers" })} />
      </Section>
    </View>
  );
}

/** One of macOS's grants, mirrored: Allowed in ok, or Allow… opening its pane in System Settings. */
function Grant(props: { title: string; granted: boolean | null; allowedWord?: string; onAllow?: () => void; testID: string }) {
  return (
    <Row testID={props.testID} title={props.title}
      trailing={<If when={props.granted !== null}>
        <If when={props.granted} else={<If when={props.onAllow}><RowButton testID={`${props.testID}-allow`} label="Allow…" accessibilityLabel={`Allow ${props.title}`} onPress={() => props.onAllow!()} /></If>}>
          <StateLine testID={`${props.testID}-state`} tone="ok" label={props.allowedWord ?? "Allowed"} />
        </If>
      </If>} />
  );
}

function Access() {
  const permissions = signal<ScreenPermission[] | null>(null);
  const agentUse = signal<AgentUseStatus | null>(null);
  const read = () => {
    const server = untracked(current);
    if (!server || !isThisMac(server)) return;
    void screenRequest(server).then((status) => permissions.set(status.permissions ?? [])).catch(() => permissions.set(null));
    void getAgentUse(server).then((status) => agentUse.set(status)).catch(() => agentUse.set(null));
  };
  // macOS gives no event for a grant: read again every 3 s while the page is open, so the row seals itself.
  effect(() => {
    if (connection() !== "connected" || !isThisMac(current())) return;
    untracked(read);
    const timer = setInterval(read, 3_000);
    cleanup(() => clearInterval(timer));
  });
  const grant = (id: string) => {
    const list = permissions();
    if (!list) return null;
    return list.find((p) => p.id === id)?.granted ?? null;
  };
  const open = (id: string) => {
    const server = untracked(current);
    if (server) void screenRequest(server, { action: "open_settings", permission: id }).then(read).catch((error) => console.warn("open settings:", error));
  };
  const browser = () => agentUse()?.browser ?? null;
  const fixBrowser = () => {
    const server = untracked(current);
    const fix = untracked(browser)?.fix;
    if (server && fix) void changeAgentUse(server, { fix }).then((next) => agentUse.set(next)).catch((error) => console.warn("chrome:", error));
  };
  return (
    <View>
      <Heading title="Privacy & access" />
      <ThisMacOnly />
      <If when={isThisMac(current())}><>
      <Section title="What agents may use on this Mac">
        <Grant testID="mac-access-screen" title="Screen recording" granted={grant("screen_recording")} onAllow={() => open("screen_recording")} />
        <Grant testID="mac-access-accessibility" title="Accessibility (click and type)" granted={grant("accessibility")} onAllow={() => open("accessibility")} />
        <Grant testID="mac-access-chrome" title="Chrome" allowedWord="Extension on" granted={browser() ? browser()!.state === "ready" : null}
          onAllow={browser()?.fix ? fixBrowser : undefined} />
      </Section>
      <Section title="New devices">
        <Row testID="mac-access-devices" title="A new phone or browser" value="Asks you first" />
      </Section>
      </></If>
    </View>
  );
}

function Pet() {
  return (
    <View>
      <Heading title="Pet" />
      <Section>
        <Row testID="mac-settings-pet" lead={{ kind: "art", src: pet().art.portrait }} title="Pet" value={pet().name} chevron onPress={() => openInWindow({ name: "settings-pet" })} />
      </Section>
    </View>
  );
}

/** The Settings window's content. */
export function MacSettings() {
  return (
    <View testID="mac-settings" class="flex-1 flex-row text-foreground">
      <View class="border-r border-border" style={{ width: SIDEBAR, paddingTop: MAC_TITLE_BAR.height! + 2, paddingHorizontal: 10 }}
        macos={{ backdrop: { material: "sidebar", blending: "behindWindow" } }} desktop={{ windowDrag: true }}>
        <MaterialTint material="sidebar" />
        <View role="tablist" class="gap-0">
          {PAGES.map((page) => <SidebarRow page={page} />)}
        </View>
      </View>
      <Scroll keyboardShouldPersistTaps="handled" class="flex-1 bg-background" contentStyle={{ paddingHorizontal: 24, paddingTop: 18, paddingBottom: 24 }}>
        <View desktop={{ windowDrag: true }} style={{ height: 4 }} />
        <If when={macSettingsPage() === "general"}><General shortcut={macShortcut} /></If>
        <If when={macSettingsPage() === "connections"}><Connections /></If>
        <If when={macSettingsPage() === "agents"}><Agents /></If>
        <If when={macSettingsPage() === "access"}><Access /></If>
        <If when={macSettingsPage() === "pet"}><Pet /></If>
      </Scroll>
    </View>
  );
}
