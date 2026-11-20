import { cleanup, colorScheme, computed, effect, host, reduceMotion, signal, Storage, untracked } from "nsh";
import { Application, GlobalShortcut, Tray, Window } from "nsh/desktop";
import { ARRIVE_COUNT_MS, menuBarItem, menuBarReaction, PEEK_MS, peekOf, stopWords, type MenuBarItem, type MenuBarReaction } from "../../core/mac/menuBar";
import type { PetState } from "../../core/settings/petPreview";
import { catNow } from "../home/Cat";
import { brain, brainChat, connection, current, ready } from "../session";
import { nav, openBrainTerminal } from "../shell/nav";
import { showAlert } from "../shell/alert";
import { tray as trayState } from "../tray/state";
import { catUpdateHint } from "../update/hint";
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

type MacParams = { release?: boolean; quickChatShortcut?: string; devScript?: { perch?: boolean; quickSend?: string; pairPhone?: boolean; settings?: MacSettingsPage; shortcutOffer?: boolean; terminal?: boolean } };

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
  let wasVisible = false;
  effect(() => {
    const visible = Window.get("main")?.state()?.visible;
    if (visible === undefined) return;
    // The Dock icon shows while a window does (§2.4): the window or Settings.
    void host.call("app.dock", { shown: visible || Window.get(MAC_SETTINGS)?.state()?.visible === true }).catch(() => {});
    // The first time the window closes, the quick chat's shortcut is offered from the cat (§2.4, `m-shortcut`).
    if (wasVisible && !visible) untracked(offerShortcutOnce);
    wasVisible = visible;
  });

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
      return setStartAtLogin(true).then(() => Storage.setItem(LOGIN_DEFAULT_KEY, "1"));
    }).catch((error) => console.warn("start at login:", error));
  }

  // The menu bar cat: a click opens the perch under it; a right-click (or ⌃-click) opens the menu.
  effect(() => {
    const now = cat();
    const play = playing();
    // The seal's red is drawn in the image, one per appearance: the app's own, as the stock host gives no menu bar
    // appearance (the face is ink on a light bar, white on a dark one).
    const dark = colorScheme() === "dark";
    const clip = play && MENU_BAR_CLIPS[play.clip === "arrive" ? (dark ? "arrive-dark" : "arrive-light") : play.clip];
    // Still images; under reduced motion the arc rests on its frame 03 (DESIGN-MAC.md §2.1).
    const still = { quiet: MENU_BAR_IMAGES.quiet, offline: MENU_BAR_IMAGES.offline, update: MENU_BAR_IMAGES.update, arc: MENU_BAR_CLIPS.arc.frames[3], needs: dark ? MENU_BAR_IMAGES["needs-dark"] : MENU_BAR_IMAGES["needs-light"] };
    const counting = now.needs > 0 && (play?.clip !== "arrive" || play.ms >= ARRIVE_COUNT_MS);
    Tray.set({
      icon: clip ? clip.frames[Math.min(play!.frame, clip.frames.length - 1)] : still[now.image],
      tooltip: connection() === "connected" ? "Mewla" : "Mewla: connecting",
      title: peek() ? [{ text: peek()! }] : counting ? [{ text: String(now.needs) }] : undefined,
      onClick: ({ bounds }) => togglePerch(bounds),
      // The cat's menu as the design draws it (DESIGN-MAC.md §2.1, `m-menu`).
      menu: [
        { label: "Open Mewla", shortcut: "mod+0", onSelect: showMainWindow },
        { label: "Quick chat", shortcut: perchShortcut() ?? undefined, onSelect: () => openPerch() },
        { type: "separator" },
        { label: "Pair a phone…", onSelect: () => void openPairPhone() },
        { label: "Settings…", shortcut: "mod+,", onSelect: openMacSettings },
        { type: "separator" },
        {
          label: "Start at login",
          checked: atLogin(),
          onSelect: toggleStartAtLogin,
        },
        { type: "separator" },
        { label: "Stop Mewla on this Mac…", onSelect: () => void stopMewla() },
        { label: "Quit Mewla", shortcut: "mod+q", onSelect: () => Application.quit() },
      ],
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
    // The page on top of the window's stack, each time it changes: what a capture of the window should show.
    let top = "";
    effect(() => {
      const route = nav.routes().at(-1) as { name: string; workerId?: string } | undefined;
      const now = route ? `${route.name}${route.workerId ? ` ${route.workerId}` : ""}` : "";
      if (now !== top) console.log(`dev script: page ${(top = now)}`);
    });
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
        }
        // Brain's host's terminal in the window (the Mac's terminal view: libghostty-vt in a web view).
        if (!terminal && script.terminal && hostId) {
          terminal = true;
          openBrainTerminal(hostId);
          console.log(`dev script: terminal ${hostId}`);
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
