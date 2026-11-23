import { cleanup, colorScheme, computed, effect, host, lifecycle, reduceMotion, signal, Storage, untracked } from "nsh";
import { newestTerminalAttach } from "../terminal/attach";
import { Application, GlobalShortcut, Tray, Window } from "nsh/desktop";
import { ARRIVE_COUNT_MS, menuBarItem, menuBarReaction, PEEK_MS, peekOf, stopWords, type MenuBarItem, type MenuBarReaction } from "../../core/mac/menuBar";
import type { PetState } from "../../core/settings/petPreview";
import { catNow } from "../home/Cat";
import { brain, brainChat, connection, current, ready } from "../session";
import { nav, openBrainTerminal, openPage, openSession } from "../shell/nav";
import { focusComposer } from "../shell/desktop/keys";
import { dayKey } from "../../core/calendar/agenda";
import { showCalendarDay } from "../calendar/CalendarPage";
import { clock } from "../calendar/clock";
import { showAlert } from "../shell/alert";
import { tray as trayState } from "../tray/state";
import { catUpdateHint } from "../update/hint";
import { appUpdateOwner } from "../update/update";
import { appUpdateMenuItem } from "./appUpdateMenu";
import { grantRequestAction } from "../../core/screen/grantRequest";
import { isMacApp, joinLocalDaemon, MAC_TITLE_BAR, showMainWindow } from "./local";
import { MENU_BAR_CLIPS, MENU_BAR_IMAGES } from "./menubarImages";
import { startMacMenus } from "./menus";
import { atLogin, readStartAtLogin, setStartAtLogin, toggleStartAtLogin } from "./login";
import { MAC_SETTINGS, macShortcut, openMacSettings, type MacSettingsPage } from "./MacSettings";
import { offerShortcutOnce, openShortcutOffer, startQuickChatShortcut } from "./QuickChatShortcut";
import { openPairPhone } from "./PairPhone";
import { openPerch, PERCH, sendToBrain, togglePerch } from "./Perch";

/** The perch's shortcut until the person picks another (DESIGN-MAC.md §2.2): ⇧⌘Space, which neither Spotlight,
 * Raycast (⌘Space) nor ChatGPT (⌥Space) holds. The host may name another (`host.params.quickChatShortcut`). */
export const DEFAULT_PERCH_SHORTCUT = "mod+shift+space";

/** The shortcut the system gave the perch, or null while another app holds it (Settings › General shows it). */
const perchShortcut = macShortcut;

const LOGIN_DEFAULT_KEY = "mewla:mac:start_at_login_default";

type MacParams = { release?: boolean; quickChatShortcut?: string; devScript?: { perch?: boolean; quickSend?: string; pairPhone?: boolean; settings?: MacSettingsPage; shortcutOffer?: boolean; terminal?: boolean; attach?: boolean; raise?: number; open?: "calendar" | "triggers" | "screen" | "computer"; trigger?: string; calendarDay?: number; calendarScene?: string; traySqueeze?: string; session?: string; size?: [number, number]; fullScreen?: boolean; minimize?: number; update?: boolean; grant?: "asked" | "open_settings" | "not_now"; imageInput?: boolean } };

/** The menu bar cat's clip playing: the arc while Brain's turn runs, or a reaction once; its frame and its time. */
type Playing = { clip: "arc" | MenuBarReaction; frame: number; ms: number };
const playing = signal<Playing | null>(null);

/** The peek's words while it shows (DESIGN-MAC.md §2.1). */
const peek = signal<string | null>(null);

/** Stop Mewla on this Mac…: asked in the window, with what stops; then the daemon stops and the app quits. */
async function stopMewla(): Promise<void> {
  showMainWindow();
  const answer = await showAlert({
    title: "Stop Mewla on this Mac?",
    description: stopWords(untracked(trayState).runningCount),
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "stop", label: "Stop Mewla", role: "destructive" },
    ],
  });
  if (answer.action !== "stop") return;
  try {
    await host.call("app.stop", {}, { timeout: 60_000 });
  } catch (error) {
    console.warn("stop mewla:", error);
    return;
  }
  Application.quit();
}

/** The Mac's own surfaces: this computer's daemon as the current server, the menu bar cat, the perch. */
export function startMac(): void {
  if (!isMacApp()) return;
  const params = host.params as MacParams;
  Window.get("main")?.set({ titleBar: MAC_TITLE_BAR, transparent: true });
  // The Dock icon while the window shows; with it closed (⌘W hides it) only the menu bar cat stays.
  let wasVisible: boolean | undefined;
  effect(() => {
    const visible = Window.get("main")?.state()?.visible;
    if (visible === undefined) return;
    // The Dock icon shows while a window is open (§2.4): the window or Settings, minimized ones too (they live in the
    // Dock; without its icon a minimized window had no way back: the Mac run saw Accessory after a minimize, 11-21).
    const open = (state: { visible: boolean; minimized: boolean } | null | undefined) => Boolean(state && (state.visible || state.minimized));
    void host.call("app.dock", { shown: open(Window.get("main")?.state()) || open(Window.get(MAC_SETTINGS)?.state()) }).catch(() => {});
    if (visible !== wasVisible) console.log(`window: main ${visible ? "shown" : "hidden"}`);
    wasVisible = visible;
  });
  // The first time the person closes the window, the quick chat's shortcut is offered from the cat (§2.4,
  // `m-shortcut`): its close button here, ⌘W in menus.ts. Their close, not the window's state, which also goes
  // hidden when it is minimized or the app hidden (⌘H).
  Window.get("main")?.on("closePress", () => untracked(offerShortcutOnce));

  // This computer's daemon is the app's computer: join it once the stored computers are read.
  let joined = false;
  effect(() => {
    if (!ready() || joined) return;
    joined = true;
    void untracked(joinLocalDaemon);
  });

  const cat = computed(() => menuBarItem({ cat: catNow(), needs: trayState().needs.length, update: Boolean(catUpdateHint()) }));
  // The menu bar cat moves only to react, once (P3): the arrival when something starts to need the person, the nod
  // when a Work comes back done; and the arc turns while Brain's turn runs. Under reduced motion nothing moves.
  let last: { item: MenuBarItem; cat: PetState } | null = null;
  effect(() => {
    const now = { item: cat(), cat: catNow() };
    const reaction = menuBarReaction(last, now);
    last = now;
    const clip = now.item.image === "arc" ? "arc" : reaction;
    untracked(() => playing.set(clip && !reduceMotion() ? { clip, frame: 0, ms: 0 } : null));
  });
  // The peek: a request that starts needing the person while the perch is closed says who asks and what for 6 s, once
  // per request; none for what was there at launch, none under reduced motion (the count only).
  const peeked = new Set<string>();
  // What arrives with the first snapshot after connecting was already there.
  let connectedAt = 0;
  effect(() => {
    if (connection() === "connected") connectedAt = Date.now();
  });
  effect(() => {
    const needs = trayState().needs;
    untracked(() => {
      const next = peekOf(peeked, needs);
      for (const need of needs) peeked.add(need.key);
      if (!next || !connectedAt || Date.now() - connectedAt < 5_000 || reduceMotion() || Window.get(PERCH)) return;
      peek.set(next.words);
      setTimeout(() => untracked(peek) === next.words && peek.set(null), PEEK_MS);
    });
  });
  effect(() => {
    const clip = playing()?.clip;
    if (!clip) return;
    const { frames, frameMs, loop } = MENU_BAR_CLIPS[clip === "arrive" ? (colorScheme() === "dark" ? "arrive-dark" : "arrive-light") : clip];
    const timer = setInterval(() => {
      const now = untracked(playing);
      if (!now) return;
      const frame = now.frame + 1;
      if (frame < frames.length) playing.set({ ...now, frame, ms: now.ms + frameMs });
      else if (loop) playing.set({ ...now, frame: 0, ms: now.ms + frameMs });
      else playing.set(null);
    }, frameMs);
    cleanup(() => clearInterval(timer));
  });
  readStartAtLogin();
  // Start at login is on by default (it is how the cat comes back, DESIGN-MAC.md §2.4), once: the person's choice
  // stands after that. Only the release; Mewla Dev never registers a login item.
  if (params.release) {
    void Storage.getItem(LOGIN_DEFAULT_KEY).then((done) => {
      if (done) return;
      // What the system says afterwards is logged: the release's first run is proven by it (macos-app.yml, `update`).
      return setStartAtLogin(true).then(() => Application.launchAtLogin()).then((on) => { console.log(`start at login: ${on ? "on" : "off"} after the first run's default`); return Storage.setItem(LOGIN_DEFAULT_KEY, "1"); });
    }).catch((error) => console.warn("start at login:", error));
  }

  // The menu bar cat: a click opens the perch under it; a right-click (or ⌃-click) opens the menu.
  const traySpec = () => {
    const now = cat();
    const play = playing();
    // The seal's red is drawn in the image, one per appearance: the app's own, as the stock host gives no menu bar
    // appearance (the face is ink on a light bar, white on a dark one).
    const dark = colorScheme() === "dark";
    const clipName = play && (play.clip === "arrive" ? (dark ? "arrive-dark" : "arrive-light") : play.clip);
    const clip = clipName ? MENU_BAR_CLIPS[clipName] : null;
    // Still images; under reduced motion the arc rests on its frame 03 (DESIGN-MAC.md §2.1).
    const still = { quiet: MENU_BAR_IMAGES.quiet, offline: MENU_BAR_IMAGES.offline, update: MENU_BAR_IMAGES.update, arc: MENU_BAR_CLIPS.arc.frames[3], needs: dark ? MENU_BAR_IMAGES["needs-dark"] : MENU_BAR_IMAGES["needs-light"] };
    const counting = now.needs > 0 && (play?.clip !== "arrive" || play.ms >= ARRIVE_COUNT_MS);
    const frame = clip ? Math.min(play!.frame, clip.frames.length - 1) : 0;
    return {
      iconName: clip ? `${clipName}:${frame}` : `${now.image}:${dark ? "dark" : "light"}`,
      icon: clip ? clip.frames[frame] : still[now.image],
      tooltip: connection() === "connected" ? "Mewla" : "Mewla: connecting",
      title: peek() ? [{ text: peek()! }] : counting ? [{ text: String(now.needs) }] : undefined,
      // The cat's menu as the design draws it (DESIGN-MAC.md §2.1, `m-menu`).
      menu: [
        { label: "Open Mewla", shortcut: "mod+0", onSelect: showMainWindow },
        { label: "Quick chat", shortcut: perchShortcut() ?? undefined, onSelect: () => openPerch() },
        { type: "separator" as const },
        { label: "Pair a phone…", onSelect: () => void openPairPhone() },
        { label: "Settings…", shortcut: "mod+,", onSelect: openMacSettings },
        { type: "separator" as const },
        {
          label: "Start at login",
          checked: atLogin(),
          onSelect: toggleStartAtLogin,
        },
        ...appUpdateMenuItem(appUpdateOwner().state(), () => appUpdateOwner().press()),
        { type: "separator" as const },
        { label: "Stop Mewla on this Mac…", onSelect: () => void stopMewla() },
        { label: "Quit Mewla", shortcut: "mod+q", onSelect: () => Application.quit() },
      ],
    };
  };
  // Set again only when what it shows changes. Each Tray.set gives the menu's rows new ids and drops the old ones'
  // handlers, so a row chosen in a menu opened before it did nothing (the walk of 11-22: Pair a phone… from the cat,
  // while each daemon snapshot set the same tray again). The key names the icon (its clip and frame, or the still);
  // JSON leaves the handlers out.
  const trayKey = computed(() => {
    const { icon: _icon, ...shown } = traySpec();
    return JSON.stringify(shown);
  });
  effect(() => {
    trayKey();
    untracked(() => {
      const { iconName: _name, ...spec } = traySpec();
      Tray.set({ ...spec, onClick: ({ bounds }) => togglePerch(bounds) });
    });
  });

  startMacMenus(perchShortcut);

  void startQuickChatShortcut(params.quickChatShortcut || DEFAULT_PERCH_SHORTCUT, () => togglePerch());

  // Mewla Dev's proof script (host.params.devScript, set only by the Dev host): once this Mac's daemon answers, the
  // perch opens as the shortcut opens it (perch) and the phone pairing window opens (pairPhone); once Brain's chat is
  // open, the perch sends one line (quickSend).
  const script = params.devScript;
  if (script) {
    let connected = false;
    let sent = false;
    let terminal = false;
    let imaged = false;
    // The page on top of the window's stack, each time it changes: what a capture of the window should show.
    let top = "";
    effect(() => {
      const route = nav.routes().at(-1) as { name: string; workerId?: string } | undefined;
      const now = route ? `${route.name}${route.workerId ? ` ${route.workerId}` : ""}` : "";
      if (now !== top) console.log(`dev script: page ${(top = now)}`);
    });
    // The app's lifecycle as NSH tells it (on a Mac `background` is hidden or covered): whether the window could draw.
    effect(() => console.log(`dev script: lifecycle ${lifecycle()}`));
    effect(() => {
      if (!current() || connection() !== "connected") return;
      const chat = Boolean(brainChat());
      const hostId = brain()?.host_worker?.id ?? "";
      untracked(() => {
        if (!connected) {
          connected = true;
          console.log("dev script: connected");
          // The dialog first: it brings the window forward, which would close an open perch (it closes on blur).
          if (script.pairPhone) openPairPhone();
          // Settings and the shortcut offer for the side-by-side captures (m-settings, m-access, m-shortcut).
          if (script.settings) openMacSettings(script.settings);
          if (script.shortcutOffer) openShortcutOffer();
          if (script.perch && !script.quickSend) togglePerch();
          // A page and the window's size for the captures beside w-cal, w-screen, w-mac, w-small and w-full.
          if (script.open) openPage(({ calendar: { name: "calendar" }, triggers: { name: "calendar" }, screen: { name: "screen" }, computer: { name: "computer" } } as const)[script.open]);
          // Calendar's own pages for the captures beside its mocks: the Triggers page (over Calendar, as its row opens
          // it), one Trigger, Month on a day `calendarDay` days from today.
          if (script.open === "triggers") nav.push({ name: "triggers" });
          if (script.trigger) nav.push({ name: "trigger", id: script.trigger });
          if (typeof script.calendarDay === "number") showCalendarDay(dayKey(untracked(clock), script.calendarDay));
          // A Session's page (the fidelity captures' busy desk, macos-fidelity.yml).
          if (script.session) openSession(script.session);
          if (script.size) Window.get("main")?.setBounds({ size: { width: script.size[0], height: script.size[1] } });
          if (script.fullScreen) Window.get("main")?.setMode("fullScreen");
          // `minimize` seconds later the window goes to the Dock, which keeps the app's icon (the run reads the policy).
          // The app's own update (macos-app.yml, `update`): once a newer release is found it is taken, as the row's button takes it.
          if (script.update) void updateProof();
          if (script.grant) void grantProof(script.grant);
          if (script.minimize) setTimeout(() => { Window.get("main")?.minimize(); console.log("dev script: minimized"); }, script.minimize * 1000);
        }
        // Brain's host's terminal in the window (the Mac's terminal view: libghostty-vt in a web view).
        if (!terminal && script.terminal && hostId) {
          terminal = true;
          openBrainTerminal(hostId);
          console.log(`dev script: terminal ${hostId}`);
          if (script.attach) void attachProof();
          // `raise` seconds later the window comes in front for a capture: shown, never focused (in the background the
          // app does not activate and takes no keys).
          if (script.raise) {
            setTimeout(() => {
              Window.get("main")?.show();
              console.log("dev script: raised");
            }, script.raise * 1000);
          }
        }
        // Image input (macos-image-input.yml): Brain's composer takes the keyboard once its chat is open; the run then
        // pastes and drags into the window as a person does.
        if (!imaged && script.imageInput && chat) {
          imaged = true;
          setTimeout(() => {
            focusComposer();
            console.log("dev script: image ready");
          }, 1500);
        }
        if (!sent && script.quickSend && chat) {
          sent = true;
          togglePerch();
          console.log(`dev script: perch sent ${sendToBrain(script.quickSend)}`);
        }
      });
    });
  }
}

/**
 * The permission card's proof (macos-app.yml, `grant`): the run asks for a grant with `mewla agent-use need`; once the card is in
 * Brain's thread it is logged, and `open_settings` or `not_now` press that button as the card does (the card's own request).
 * Every state it shows is logged.
 */
async function grantProof(press: "asked" | "open_settings" | "not_now"): Promise<void> {
  const card = () => (brainChat()?.current.conversation?.events ?? []).filter((event) => event.source === "grant_request").at(-1);
  const until = async (ok: () => boolean, ms = 90_000) => {
    for (let waited = 0; !ok(); waited += 250) {
      if (waited > ms) return false;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
    return true;
  };
  if (!(await until(() => Boolean(card())))) return void console.log("dev script: grant stopped (no card)");
  // The run's `mewla agent-use need` may still be answering: the card pressed is the newest, once the thread has settled.
  await new Promise((resolve) => setTimeout(resolve, 8_000));
  console.log(`dev script: grant card ${card()!.status} ${card()!.grant_permission} "${card()!.body}"`);
  const server = untracked(current);
  if (press === "asked" || !server) return;
  try {
    await grantRequestAction(server, card()!.id, press);
    await until(() => card()!.status !== "asked", 15_000);
    console.log(`dev script: grant pressed ${press}: card ${card()!.status}`);
  } catch (error) {
    console.log(`dev script: grant press failed ${error instanceof Error ? error.message : String(error)}`);
  }
}

/**
 * The update proof (macos-app.yml, `update`): a release app finds the newer Mewla a loopback manifest offers, waits
 * `hold` seconds so the run can capture Settings with its row, then takes it as the row's button does; the app
 * restarts as the new version. Every state it goes through is logged, and the new app logs that it is current.
 */
async function updateProof(hold = 12): Promise<void> {
  const owner = appUpdateOwner();
  effect(() => console.log(`dev script: update ${owner.state().kind}`));
  const until = async (ok: () => boolean, ms = 90_000) => {
    for (let waited = 0; !ok(); waited += 250) {
      if (waited > ms) return false;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
    return true;
  };
  if (!(await until(() => ["available", "current", "checkFailed"].includes(owner.state().kind)))) return void console.log("dev script: update stopped (no answer)");
  const state = owner.state();
  if (state.kind !== "available") return void console.log(`dev script: update none (${state.kind})`);
  console.log(`dev script: update found ${state.update.version}`);
  // The cat's menu row for this state, from the function the menu's items come from (a run cannot open a menu bar item's menu).
  console.log(`dev script: menu row ${JSON.stringify(appUpdateMenuItem(state, () => {}).flatMap((item) => ("label" in item ? [item.label] : [])))}`);
  await new Promise((resolve) => setTimeout(resolve, hold * 1000));
  owner.press();
}

/**
 * Mewla Dev's terminal attach proof (macos-app.yml, `attach`): through the terminal's own Attach, one file that goes up
 * (its path typed at the prompt), one that can't (a name over the 1024 bytes uploads allow: a failed chip), then a large one
 * still uploading when the run captures the window; then Cancel on the large one and Retry on the failed one, as their
 * buttons call them.
 */
async function attachProof(): Promise<void> {
  const until = async (ok: () => boolean, ms = 60_000) => {
    for (let waited = 0; !ok(); waited += 250) {
      if (waited > ms) throw new Error("timed out");
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  };
  try {
    await until(() => Boolean(newestTerminalAttach()?.uploads.canAttach()));
    const uploads = newestTerminalAttach()!.uploads;
    const settled = () => uploads.items().every((item) => item.status === "failed");
    uploads.add([new File(["Mewla Dev's attach proof\n"], "proof notes.txt", { type: "text/plain" })]);
    console.log("dev script: attach sent");
    await until(settled);
    uploads.add([new File(["refused"], `${"n".repeat(1100)}.txt`, { type: "text/plain" })]);
    await until(() => uploads.items().some((item) => item.status === "failed"));
    console.log(`dev script: attach failed ${uploads.items().find((item) => item.status === "failed")?.error ?? ""}`);
    // 1 GB: still going when the run has captured the window (384 MB went up in 6 s on the runner).
    uploads.add([new File([new Uint8Array(1 << 30)], "screen recording.mov", { type: "video/quicktime" })]);
    await until(() => uploads.items().some((item) => item.status === "uploading"));
    console.log("dev script: attach uploading");
    // Long enough for the run to capture the chip, short enough that the 1 GB one is still going (a busy runner's timers lag).
    await new Promise((resolve) => setTimeout(resolve, 3000));
    const large = uploads.items().find((item) => item.status === "uploading");
    if (large) {
      uploads.remove(large.id);
      console.log(`dev script: attach cancelled (${uploads.items().some((item) => item.id === large.id) ? "still listed" : "gone"})`);
    } else console.log("dev script: attach cancel missed (the large one was up already)");
    const failed = uploads.items().find((item) => item.status === "failed");
    if (failed) {
      uploads.retry(failed.id);
      await until(() => uploads.items().find((item) => item.id === failed.id)?.status === "failed");
      console.log(`dev script: attach retried, failed again: ${uploads.items().find((item) => item.id === failed.id)?.error ?? ""}`);
    }
    console.log("dev script: attach done");
  } catch (error) {
    console.log(`dev script: attach stopped ${error instanceof Error ? error.message : String(error)}`);
  }
}
