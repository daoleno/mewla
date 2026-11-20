import { colorScheme, effect, keymap, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { createNavStack, NavStack } from "nsh/base";
import { currentWindow, displays, Window } from "nsh/desktop";
import { PERCH as FRAME, perchOrigin } from "../../core/mac/menuBar";
import { HomeCat } from "../home/Cat";
import { Icon } from "../kit/Icon";
import { StateControl } from "../tray/StateControl";
import { Home } from "../home/Home";
import { brainChat, current } from "../session";
import { showMainWindow } from "./local";
import { goHome, openPage, trayOpen } from "../shell/nav";

/** The perch's id among the app's windows (DESIGN-MAC.md §2.2). */
export const PERCH = "perch";
/** The window: the panel and, above it, the pet's overhang. */
const SIZE = { width: FRAME.width, height: FRAME.height + FRAME.overhang };

type Bounds = { origin: { x: number; y: number }; size: { width: number; height: number } };

/** Where the menu bar item was last clicked: the perch opens under it (the shortcut gives no item frame). */
let lastItem: Bounds | null = null;

/**
 * Detached (DESIGN-MAC.md §2.2): dragged away from the menu bar by its header, or Window › Detach Quick Chat, the
 * perch stays as a floating window (on top, on every Space) until closed, with × in ⤢'s place.
 */
export const perchDetached = signal(false);

export function detachPerch(): void {
  const perch = untracked(() => Window.get(PERCH));
  if (!perch || untracked(perchDetached)) return;
  perchDetached.set(true);
  perch.set({ closeOnBlur: false });
}

/** ⌘O and ⤢: the window on Brain's thread, where the perch was (one thread, one draft); the perch closes. */
export function openInWindow(): void {
  untracked(() => Window.get(PERCH))?.close();
  goHome();
  showMainWindow();
}

/** The perch's header (50, the popover material, a hairline under it): "Brain" and the computer, the state control, ⤢. */
function PerchHeader() {
  return (
    <View testID="perch-header" class="flex-row items-center border-b-[0.5px] border-border" style={{ height: FRAME.header, paddingLeft: 60, paddingRight: 10, gap: 8 }}
      macos={{ backdrop: { material: "popover", blending: "behindWindow" } }} desktop={{ windowDrag: true }}>
      <View class="min-w-0 shrink flex-row items-baseline">
        <Text role="header" class="text-foreground" style={{ fontSize: 14, fontWeight: "600" }}>Brain</Text>
        <If when={current()?.name}><Text testID="perch-computer" class="shrink text-muted-foreground line-clamp-1" style={{ fontSize: 12, marginLeft: 6 }}>{current()!.name}</Text></If>
      </View>
      <View class="flex-1" />
      {/* The words with their dot, no pill (as in the phone's capsule header). */}
      <StateControl capsule={{ short: () => false }} onPairAgain={() => { openInWindow(); openPage({ name: "computer" }, { name: "pair" }); }} />
      <If when={perchDetached()} else={<Button testID="perch-open-window" accessibilityLabel="Open in window" onPress={openInWindow}
          class="items-center justify-center rounded-md hover:bg-accent active:bg-accent" style={{ width: 26, height: 26 }}>
          <Icon name="expand" size={16} tone="ink" />
        </Button>}><Button testID="perch-close" accessibilityLabel="Close" onPress={() => untracked(() => Window.get(PERCH))?.close()}
          class="items-center justify-center rounded-md hover:bg-accent active:bg-accent" style={{ width: 26, height: 26 }}>
          <Icon name="close" size={16} tone="ink" />
        </Button></If>
    </View>
  );
}

/**
 * The perch (DESIGN-MAC.md §2.2): the phone's Brain home in a panel under the menu bar cat, with its own header, the
 * end of the thread and the composer, and the person's pet perched on its top edge. The window is transparent: the
 * panel draws the Mac's edge and rim, the system its shadow. Esc closes the tray when it is open, else the perch; a
 * click outside closes it.
 */
function Perch() {
  const stack = createNavStack<{ name: "home" }>([{ name: "home" }]);
  // A perch opens under the cat; moved more than a few points by its header, it is detached.
  perchDetached.set(false);
  let opened: { x: number; y: number } | null = null;
  effect(() => {
    const at = currentWindow().state()?.bounds.origin;
    if (!at) return;
    if (!opened) return void (opened = at);
    if (Math.abs(at.x - opened.x) + Math.abs(at.y - opened.y) > 8) untracked(detachPerch);
  });
  keymap.bind({
    "mod+o": openInWindow,
    escape: () => (untracked(trayOpen) ? trayOpen.set(false) : untracked(() => Window.get(PERCH))?.close()),
  });
  const dark = () => colorScheme() === "dark";
  return (
    <View testID="perch" class="flex-1">
      <View testID="perch-panel" class="flex-1 overflow-hidden bg-background"
        style={{ marginTop: FRAME.overhang, borderRadius: FRAME.radius, borderWidth: 0.5, borderColor: dark() ? "rgba(0,0,0,0.6)" : "rgba(0,0,0,0.12)" }}>
        <NavStack state={stack} style={{ flex: 1 }}>{() => <Home perch={{ header: () => <PerchHeader /> }} />}</NavStack>
        <View class="absolute inset-0"
          style={{ pointerEvents: "none", borderRadius: FRAME.radius - 0.5, borderWidth: 1, borderColor: dark() ? "rgba(255,255,255,0.1)" : "rgba(255,255,255,0.7)" }} />
      </View>
      <View testID="perch-cat" class="absolute" style={{ left: FRAME.catX, top: 0, width: FRAME.cat, height: FRAME.cat }}>
        <HomeCat size={FRAME.cat} box={FRAME.cat} />
      </View>
    </View>
  );
}

function origin(item: Bounds | null): { x: number; y: number } | "center" {
  const screens = displays();
  const inside = (b: Bounds, p: { x: number; y: number }) =>
    p.x >= b.origin.x && p.x < b.origin.x + b.size.width && p.y >= b.origin.y && p.y < b.origin.y + b.size.height;
  const display = (item && screens.find((d) => inside(d.bounds, item.origin))) ?? screens.find((d) => d.primary) ?? screens[0];
  if (!display) return "center";
  const area = display.visibleBounds;
  // Without a click, under the right end of the menu bar, where the item sits.
  const at = item ?? { origin: { x: area.origin.x + area.size.width - 120, y: display.bounds.origin.y }, size: { width: 24, height: area.origin.y - display.bounds.origin.y } };
  return perchOrigin(
    { x: at.origin.x, y: at.origin.y, width: at.size.width, height: at.size.height },
    { x: area.origin.x, y: area.origin.y, width: area.size.width, height: area.size.height },
  );
}

/**
 * A popover hanging from the menu bar item (the shortcut offer, DESIGN-MAC.md §2.4): its left edge at the item's,
 * 6 pt under the bar, inside the screen. The stock host gives no item frame: the last click's, else the bar's right end.
 */
export function underItem(width: number): { x: number; y: number } | "center" {
  const screens = displays();
  const inside = (b: Bounds, p: { x: number; y: number }) =>
    p.x >= b.origin.x && p.x < b.origin.x + b.size.width && p.y >= b.origin.y && p.y < b.origin.y + b.size.height;
  const display = (lastItem && screens.find((d) => inside(d.bounds, lastItem!.origin))) ?? screens.find((d) => d.primary) ?? screens[0];
  if (!display) return "center";
  const area = display.visibleBounds;
  const x = lastItem ? lastItem.origin.x : area.origin.x + area.size.width - 120;
  return { x: Math.round(Math.max(area.origin.x + 8, Math.min(x, area.origin.x + area.size.width - width - 8))), y: area.origin.y + 6 };
}

/** Opens the perch under the item (`item`: the click's frame), or closes it when it is open: the shortcut toggles. */
export function togglePerch(item?: Bounds | null): void {
  if (item) lastItem = item;
  const open = untracked(() => Window.get(PERCH));
  if (open) return void open.close();
  openPerch();
}

export function openPerch(): void {
  Window.open(Perch, {
    id: PERCH,
    kind: "popup",
    title: "Mewla",
    defaultSize: SIZE,
    defaultPosition: origin(lastItem),
    minSize: { width: FRAME.minWidth, height: FRAME.minHeight + FRAME.overhang },
    resizable: true,
    workspaces: "all",
    level: "floating",
    closeOnBlur: true,
    closeOnEscape: false,
    transparent: true,
    shadow: true,
  }).focus();
}

/** Sends a line to Brain as the perch's composer does; false while Brain's chat is not open. */
export function sendToBrain(text: string): boolean {
  const value = text.trim();
  const chat = untracked(brainChat);
  if (!value || !chat) return false;
  chat.send(value);
  return true;
}
