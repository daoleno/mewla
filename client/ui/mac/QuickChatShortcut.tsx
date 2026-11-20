import { keymap, signal, Storage, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { GlobalShortcut, Window } from "nsh/desktop";
import { heldBy, knownHolder, normalise, shortcutOf, type HeardKey } from "../../core/mac/shortcut";
import { macShortcut } from "./MacSettings";
import { underItem } from "./Perch";

/**
 * The quick chat's shortcut (DESIGN-MAC.md §2.2, §2.4): ⇧⌘Space until the person records another. A shortcut another
 * app holds shows as taken and is never set silently; the one that works stays until a new one is taken.
 */
const SHORTCUT_KEY = "mewla:mac:quick_chat_shortcut";
const OFFERED_KEY = "mewla:mac:shortcut_offered";
export const SHORTCUT_OFFER = "shortcut-offer";
const OFFER = { width: 280, height: 134 };

/** What the perch's shortcut does (mac.ts: toggles the perch). */
let onPress: () => void = () => {};
/** The keystroke registered now, held by Mewla. */
let registered: string | null = null;
/** The one asked for, also while another app holds it (the offer names it as taken). */
export const wantedShortcut = signal<string>("");

/** Registers the stored shortcut (else `fallback`) once at start. */
export async function startQuickChatShortcut(fallback: string, press: () => void): Promise<void> {
  onPress = press;
  const stored = await Storage.getItem(SHORTCUT_KEY).catch(() => null);
  const wanted = stored || fallback;
  wantedShortcut.set(wanted);
  const taken = await GlobalShortcut.register(wanted, () => onPress());
  registered = taken ? wanted : null;
  macShortcut.set(registered);
  console.log(taken ? `perch: ${wanted} registered` : `perch: ${wanted} is held by another app`);
}

/** Takes `next` for the quick chat: Mewla's once the system gives it, else why not (and the old one stays). */
export async function changeQuickChatShortcut(next: string): Promise<{ ok: true } | { ok: false; heldBy: string }> {
  if (registered && normalise(registered) === normalise(next)) return { ok: true };
  // The system does not refuse ⌘C or ⌘Q: a global shortcut would take them from every app.
  const known = knownHolder(next);
  if (known) return { ok: false, heldBy: known };
  const taken = await GlobalShortcut.register(next, () => onPress());
  if (!taken) return { ok: false, heldBy: heldBy(next) };
  if (registered) GlobalShortcut.unregister(registered);
  registered = next;
  macShortcut.set(next);
  wantedShortcut.set(next);
  await Storage.setItem(SHORTCUT_KEY, next).catch(() => {});
  return { ok: true };
}

/**
 * The recorder (30 tall, radius 10, a 1.5 pt ink ring, 15/600): the shortcut as key caps; while recording it reads
 * "Type a shortcut" and takes the next keystroke with ⌘, ⌃ or ⌥ (Esc stops). `compact`: Settings' key cap.
 */
export function ShortcutRecorder(props: { recording: () => boolean; onRecording: (on: boolean) => void; compact?: boolean; testID?: string }) {
  const problem = signal("");
  const shown = () => macShortcut() ?? wantedShortcut();
  const heard = (event: HeardKey) => {
    if (!untracked(props.recording)) return;
    if (event.key === "Escape" || event.code === "Escape") return props.onRecording(false);
    const next = shortcutOf(event);
    if (!next) return;
    void changeQuickChatShortcut(next).then((result) => {
      problem.set(result.ok ? "" : `${keymap.label(next)} is held by ${result.heldBy}`);
      if (result.ok) props.onRecording(false);
    });
  };
  const words = () => (props.recording() ? "Type a shortcut" : shown() ? keymap.label(shown()) : "None");
  return (
    <View class="gap-1.5" onKeyDown={heard} captureKeys={props.recording() ? "all" : "none"}>
      <Button testID={props.testID} accessibilityLabel={`Quick chat shortcut, ${words()}`} onPress={() => props.onRecording(!untracked(props.recording))}
        class={props.compact ? `items-center justify-center rounded-md px-2 ${props.recording() ? "border border-foreground" : "bg-muted"}` : "items-center justify-center border-foreground"}
        style={props.compact ? { height: 24 } : { height: 30, borderRadius: 10, borderWidth: 1.5 }}>
        <Text class={props.compact ? "text-sub text-muted-foreground" : "text-foreground"} style={props.compact ? undefined : { fontSize: 15, fontWeight: "600" }}>{words()}</Text>
      </Button>
      <If when={problem() || (!macShortcut() && wantedShortcut())}>
        <Text testID={props.testID ? `${props.testID}-taken` : undefined} class="text-caption text-muted-foreground">{problem() || `${keymap.label(wantedShortcut())} is held by ${heldBy(wantedShortcut())}`}</Text>
      </If>
    </View>
  );
}

/** The offer's content: "Quick chat", the recorder, Change and Done. */
function ShortcutOffer() {
  const recording = signal(false);
  const close = () => untracked(() => Window.get(SHORTCUT_OFFER))?.close();
  return (
    <View testID="shortcut-offer" class="flex-1 overflow-hidden text-foreground" style={{ borderRadius: 16, padding: 16, gap: 0 }}
      macos={{ backdrop: { material: "popover", blending: "behindWindow" } }}>
      <Text role="header" class="font-bold" style={{ fontSize: 17, lineHeight: 22, marginBottom: 6 }}>Quick chat</Text>
      <ShortcutRecorder testID="shortcut-recorder" recording={recording} onRecording={(on) => recording.set(on)} />
      <View class="flex-1" />
      <View class="flex-row justify-end" style={{ gap: 8 }}>
        <Button testID="shortcut-change" onPress={() => recording.set(true)} class="items-center justify-center bg-muted px-3.5 hover:opacity-80" style={{ height: 32, borderRadius: 10 }}>
          <Text class="font-semibold" style={{ fontSize: 14 }}>Change</Text>
        </Button>
        <Button testID="shortcut-done" onPress={close} class="items-center justify-center bg-primary px-3.5 hover:opacity-90" style={{ height: 32, borderRadius: 10 }}>
          <Text class="font-semibold text-primary-foreground" style={{ fontSize: 14 }}>Done</Text>
        </Button>
      </View>
    </View>
  );
}

export function openShortcutOffer(): void {
  Window.open(ShortcutOffer, {
    id: SHORTCUT_OFFER,
    kind: "popup",
    title: "Quick chat",
    defaultSize: OFFER,
    defaultPosition: underItem(OFFER.width),
    resizable: false,
    workspaces: "all",
    level: "floating",
    closeOnBlur: true,
    closeOnEscape: false,
    transparent: true,
    shadow: true,
  }).focus();
}

/** The first time the window closes, the offer hangs from the cat (once ever). */
export function offerShortcutOnce(): void {
  void Storage.getItem(OFFERED_KEY).then((done) => {
    if (done) return;
    void Storage.setItem(OFFERED_KEY, "1").catch(() => {});
    openShortcutOffer();
  }).catch(() => {});
}
