import { announce, cleanup, effect, Image, lifecycle, platform, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { fixButtonKind, type FixRow } from "../../core/computer/needsFixing";
import { failureText } from "../../core/more/daemon";
import { setServerAutoConnect, type StoredServer } from "../../core/servers";
import { changeAgentUse, installDaemonUpdate } from "../../core/settings/computer";
import { computerProblemsMayChange } from "../../core/settings/computerModel";
import { connectionTone, fixRowLead, fixRowTone } from "../../core/computer/colour";
import { color } from "../kit/colors";
import { Icon, type IconName } from "../kit/Icon";
import { Notice, Page, PillButton, Row, Section } from "../kit/kit";
import { stateTextColor, tile } from "../kit/RowTile";
import { followFriends, friendsEnabled, unread } from "../more/friends/state";
import { flowState } from "../more/extensions/plugins";
import type { MorePage, PageNav } from "../more/routes";
import { currentPathMark, forgetConnectionIssue, probeComputers, removedNotice, statusWord } from "../more/settings/computers";
import { otherState } from "./computers";
import { pet } from "../pets/pets";
import { connection, current, resumeConnection, servers, shownConnection, switchTo, transportPath } from "../session";
import { goHome, nav } from "../shell/nav";
import { markComputerUpdateSeen } from "../update/hint";
import { appUpdateWaiting } from "../update/update";
import { computerAgentUse, computerOptionalUpdate, computerUpdate, fixRows, readComputer } from "./state";

export { catNeedsYou } from "./state";

/** The header block's connection line (SPEC §1.3: the computer page says the connection in its header). */
const WORDS = { connected: "Connected", connecting: "Connecting…", waiting: "Offline", idle: "Not connected" } as const;

/**
 * The rare places, each with its value (SPEC §11 Computer page row) where the phone already knows it: Extensions'
 * connected accounts once listed, Friends' unread. Screen, Stats and Resources would each need a request of their own.
 */
const RARE: { title: string; icon: IconName; route: MorePage; lab?: boolean; value?: () => string | undefined }[] = [
  { title: "Screen", icon: "eye", route: { name: "screen" } },
  { title: "Extensions", icon: "plugins", route: { name: "extensions" }, value: () => (flowState().loaded && flowState().accounts.length ? String(flowState().accounts.length) : undefined) },
  { title: "Stats", icon: "stats", route: { name: "stats" } },
  { title: "Resources", icon: "resources", route: { name: "resources" } },
  { title: "Friends", icon: "people", route: { name: "friends" }, lab: true, value: () => (unread() > 0 ? `${unread()} new` : undefined) },
];

/** Friends is asked about once, the first time the computer page shows (not at every start). */
let followingFriends = false;

/** What a long press on the connection line revealed (how the phone reaches the computer), for a few seconds. */
const pathDetail = signal("");
let pathTimer: ReturnType<typeof setTimeout> | null = null;

function showPathDetail(detail: string): void {
  pathDetail.set(detail);
  announce(detail);
  if (pathTimer) clearTimeout(pathTimer);
  pathTimer = setTimeout(() => pathDetail.set(""), 4_000);
}

/** Another computer: one tap switches to it; the stack returns to home, which says Connecting… until it is up. */
function useComputer(server: StoredServer): void {
  void setServerAutoConnect(server.id, true);
  void switchTo(server).then(() => goHome());
}

/** "Connected · 18 ms" while the phone has measured the way there, else "Connected · LAN" (how it reaches it), else the word. */
function connectionTail(): string {
  if (connection() !== "connected") return "";
  const rtt = transportPath()?.rttMs;
  if (rtt !== null && rtt !== undefined && Number.isFinite(rtt) && rtt > 0) return `${Math.round(rtt)} ms`;
  return currentPathMark()?.label ?? "";
}

/** The header block (SPEC §11 Computer page): the 72 pt cat beside the name at display 32/700, the connection line under it. */
function CatHeader(props: { nav: PageNav }) {
  const mark = () => currentPathMark();
  return (
    <View class="gap-1 pt-2">
      <View class="flex-row items-center gap-3">
        {/* The cat does not move here: its portrait, sitting (SPEC §5), not the nap the idle header shows. */}
        <Image testID="computer-cat" src={pet().art.portrait} accessibilityElementsHidden style={{ width: 72, height: 72 }} />
        <View class="min-w-0 flex-1 gap-0.5">
          <Button testID="computer-details" accessibilityLabel={`${current()?.name ?? "This computer"}, details`} onPress={() => current() && props.nav.push({ name: "settings-computer", id: current()!.id })}
            class="self-stretch active:opacity-70">
            <Text role="header" class="text-display font-bold tracking-display line-clamp-1">{current()?.name || current()?.url || "Computer"}</Text>
          </Button>
          <View testID="computer-connection" class="flex-row items-center gap-2" onLongPress={mark() ? () => showPathDetail(mark()!.detail) : undefined}
            accessible accessibilityLabel={[WORDS[shownConnection()], connectionTail()].filter(Boolean).join(", ")}>
            {/* Connected is green; this computer offline is red with its word, since work stops (SPEC §10.4); else a soft ring. */}
            <If when={connectionTone(shownConnection()) === "off"} else={<View testID="computer-connection-dot" class="size-2 rounded-full" style={{ backgroundColor: color(connectionTone(shownConnection()) === "ok" ? "ok" : "seal") }} />}><View testID="computer-connection-dot" class="size-2 rounded-full border-2 border-muted-foreground" /></If>
            {/* One space each side of the dot: "Connected · LAN", the row's gap is only the status dot's. */}
            <View class="min-w-0 shrink flex-row">
              <Text class="text-sub" style={{ color: color(stateTextColor(connectionTone(shownConnection()))) }}>{WORDS[shownConnection()]}</Text>
              <If when={connectionTail()}><Text testID="computer-path" class="min-w-0 shrink text-sub text-muted-foreground line-clamp-1">{` · ${connectionTail()}`}</Text></If>
            </View>
          </View>
        </View>
      </View>
      <If when={pathDetail()}><Text testID="computer-path-detail" class="text-sub text-muted-foreground">{pathDetail()}</Text></If>
    </View>
  );
}

/**
 * The cat's page (soft-final SPEC §3.6): this computer (its name opens its details, its
 * connection), what needs fixing there and its one fix, the other computers (one tap switches,
 * ⓘ opens details), Pair a computer, the rare places, then Settings.
 */
export function ComputerPage(props: {
  nav: PageNav;
  /** "Claude is not signed in" › Switch: the Brain host sheet, which belongs to Brain's menu. */
  onSwitchExecutor: () => void;
}) {
  const busy = signal("");
  const notes = signal<Record<string, string>>({});
  const note = (key: string, text: string) => notes.set({ ...notes(), [key]: text });

  if (!followingFriends) {
    followingFriends = true;
    followFriends();
  }

  // Each computer is asked again whenever this page is shown again: back from a page above it,
  // the app back in front, or a computer added or removed.
  const depth = untracked(() => {
    const routes = props.nav.routes?.() ?? [];
    const at = routes.findIndex((route) => route.name === "computer");
    return at >= 0 ? at + 1 : routes.length;
  });
  let shownAs = "";
  effect(() => {
    const top = (props.nav.routes?.().length ?? depth) === depth;
    const key = `${top && lifecycle() === "active"}|${servers().map((s) => `${s.id}@${s.url}`).join(",")}`;
    untracked(() => {
      if (key === shownAs) return;
      shownAs = key;
      if (!key.startsWith("true|")) return;
      void probeComputers();
      if (connection() === "connected") readComputer();
    });
  });
  // Shown with the current computer's update row: the cat's dot for that version goes.
  effect(() => {
    const top = (props.nav.routes?.().length ?? depth) === depth;
    if (top && lifecycle() === "active" && computerOptionalUpdate()) untracked(markComputerUpdateSeen);
  });

  // While what it shows can change (an update installing, a fix under way), read again.
  let timer: ReturnType<typeof setInterval> | null = null;
  effect(() => {
    if (timer) clearInterval(timer);
    timer = null;
    const changing = connection() === "connected" && (computerUpdate()?.updating || computerProblemsMayChange(computerAgentUse()));
    if (changing) timer = setInterval(readComputer, computerUpdate()?.updating ? 3_000 : 5_000);
  });
  cleanup(() => timer && clearInterval(timer));

  async function act(key: string, work: () => Promise<void>): Promise<void> {
    if (busy()) return;
    busy.set(key);
    note(key, "");
    try {
      await work();
    } catch (error) {
      note(key, failureText(error, "That didn't finish"));
    } finally {
      busy.set("");
    }
  }

  function fix(row: FixRow): void {
    const server = current();
    const action = row.action;
    if (!server || !action) return;
    switch (action.kind) {
      case "retry":
        void setServerAutoConnect(server.id, true);
        forgetConnectionIssue();
        return resumeConnection();
      case "pair":
        return nav.push({ name: "pair" });
      case "switch_executor":
        return props.onSwitchExecutor();
      case "update":
        return void act(row.key, async () => {
          const next = await installDaemonUpdate(server);
          computerUpdate.set({ ...next, updating: true });
        });
      case "agent_use":
        return void act(row.key, async () => {
          computerAgentUse.set(await changeAgentUse(server, { fix: action.fix }));
          const where = server.name || "the computer";
          if (action.fix === "install_extension") note(row.key, `Load the Mewla folder in Chrome on ${where}: Developer mode, then Load unpacked.`);
          if (action.fix === "open_settings") note(row.key, `Turn on Mewla in the settings that opened on ${where}.`);
        });
    }
  }

  const others = () => servers().filter((s) => s.id !== current()?.id);
  const optional = computerOptionalUpdate;
  const updateRow = (): FixRow | null => (optional() ? { key: "update-optional", title: `Update available · ${optional()}`, button: "Update", action: { kind: "update" }, blocking: false } : null);

  return (
    // The header block is the page's own title: the name joins Back in the capsule once it has scrolled away.
    <Page title={current()?.name || current()?.url || "Computer"} display hero={<CatHeader nav={props.nav} />} testID="computer-page">
      <If when={removedNotice()}><Notice testID="removed-notice" tone="failed" title={removedNotice()!.title} detail={removedNotice()!.detail} action="OK" onAction={() => removedNotice.set(null)} /></If>

      <If when={fixRows().length || updateRow()}><Section title="Needs fixing" testID="needs-fixing">
          {[...fixRows(), ...(updateRow() ? [updateRow()!] : [])].map((row) => (
            <View>
              <Row
                testID={`fix-${row.key}`}
                lead={fixRowLead(row)}
                title={row.title}
                titleTone={fixRowTone(row)}
                trailing={<If when={row.button && row.action}><PillButton testID={`fix-${row.key}-button`} title={row.button!} small kind={row.key === "update-optional" ? "ghost" : fixButtonKind(fixRows())}
                    busy={busy() === row.key} disabled={Boolean(busy())} onPress={() => fix(row)} /></If>}
              />
              <If when={notes()[row.key]}><View class="px-4 pb-3"><Text class="text-sub text-muted-foreground" android={{ accessibilityLiveRegion: "polite" }}>{notes()[row.key]}</Text></View></If>
            </View>
          ))}
        </Section></If>

      <Section title="Computers" testID="computers">
        {/* This computer first: its row and its ⓘ open its details (it is already the one in use). */}
        <If when={current()}><Row
            testID={`computer-${current()!.id}`}
            lead={tile("desktop")}
            title={current()!.name || current()!.url}
            subtitle={statusWord(current()!) || undefined}
            onPress={() => props.nav.push({ name: "settings-computer", id: current()!.id })}
            trailing={
              <Button testID={`computer-${current()!.id}-info`} accessibilityLabel={`${current()!.name || "This computer"} details`} onPress={() => props.nav.push({ name: "settings-computer", id: current()!.id })}
                class="size-11 items-center justify-center active:opacity-70">
                <Icon name="info" size={22} tone="soft" />
              </Button>
            }
          /></If>
        {others().map((server) => {
          const state = () => otherState(server);
          return (
            <View class={state().dim ? "opacity-[0.55]" : ""}>
              <Row
                testID={`computer-${server.id}`}
                lead={tile("desktop")}
                title={server.name || server.url}
                subtitle={state().word || undefined}
                accessibilityLabel={[server.name || server.url, state().word].filter(Boolean).join(", ")}
                accessibilityHint="Switch to this computer"
                onPress={() => useComputer(server)}
                trailing={
                  <View class="flex-row items-center gap-1">
                    <If when={state().needsYou}><View testID={`computer-${server.id}-dot`} class="size-2.5 rounded-full bg-seal" /></If>
                    <Button testID={`computer-${server.id}-info`} accessibilityLabel={`${server.name || "Computer"} details`} onPress={() => props.nav.push({ name: "settings-computer", id: server.id })}
                      class="size-11 items-center justify-center active:opacity-70">
                      <Icon name="info" size={22} tone="soft" />
                    </Button>
                  </View>
                }
              />
            </View>
          );
        })}
        <Row testID="pair-another" lead={tile("add")} title="Pair a computer" chevron onPress={() => nav.push({ name: "pair" })} />
      </Section>

      <Section>
        {/* Friends is a phone Lab: v0.3.1's web app had none. */}
        {RARE.filter((place) => !place.lab || (platform.os !== "web" && friendsEnabled())).map((place) => (
          <Row testID={`computer-${place.route.name}`} lead={tile(place.icon)} title={place.title} value={place.value?.()} chevron onPress={() => props.nav.push(place.route)} />
        ))}
      </Section>

      <Section>
        {/* This app's update waits there: the same ink dot as the cat's, until it is installed. */}
        <Row testID="computer-settings" lead={tile("settings")} title="Settings" accessibilityLabel={appUpdateWaiting() ? "Settings, Update available" : "Settings"} chevron onPress={() => props.nav.push({ name: "settings" })}
          trailing={<If when={appUpdateWaiting()}><View testID="computer-settings-update" class="size-2.5 rounded-full bg-primary" /></If>} />
      </Section>
      <View class="h-6" />
    </Page>
  );
}
