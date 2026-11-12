import { computed, effect, keymap, Scroll, signal, Text, untracked, View } from "nsh";
import { Button, Command, Dialog } from "nsh/base";
import { buildPaletteItems, filterPaletteItems, type PaletteAction, type PaletteItem } from "../../../core/web/palette";
import {
  desktopShortcutRows,
  GLOBAL_KEYSTROKES,
  GO_KEYSTROKES,
  GO_SEQUENCE_MS,
  keyOfKeystroke,
  modKeyLabel,
  resolveGlobalKey,
  type DesktopCommand,
} from "../../../core/web/shortcuts";
import { Icon } from "../../kit/Icon";
import { startNewChat } from "../../screens/Brain";
import { newSessionOpen } from "../../screens/NewSession";
import { workers } from "../../session";
import { workSurface } from "../../work/state";
import { brainNav, desktop, openPlace, openSession, selectedTab } from "../Shell";
import { focusComposer, terminalOwnsKeys } from "./keys";

/** The ⌘K palette and the `?` cheat sheet are open. */
export const paletteOpen = signal(false);
export const shortcutsOpen = signal(false);
const pendingGo = signal(false);

const mod = () => modKeyLabel(keymap.label("mod+k").includes("⌘"));

function run(action: PaletteAction | DesktopCommand) {
  paletteOpen.set(false);
  switch (action.kind) {
    case "go": return openPlace({ key: action.key } as Parameters<typeof openPlace>[0]);
    case "palette": return paletteOpen.set(true);
    case "shortcuts": return shortcutsOpen.set(true);
    case "new-session": return newSessionOpen.set(true);
    case "new-brain-chat":
      openPlace({ key: "brain" });
      return void startNewChat();
    case "focus-composer": return focusComposer();
    case "session": return openSession(action.workerId);
    case "work":
      if (action.sessionId) return openSession(action.sessionId);
      selectedTab.set(0);
      return brainNav.setRoutes([brainNav.routes()[0], { name: "workDetail", workId: action.workId }]);
  }
}

/** A key the shell bound: what the keyboard model makes of it. */
function pressed(keystroke: string) {
  const result = resolveGlobalKey(keyOfKeystroke(keystroke), { pendingGo: untracked(pendingGo), target: null });  pendingGo.set(result.pendingGo);
  if (result.command) run(result.command);
}

/**
 * The desktop's keys, through NSH's keymap: ⌘K / Ctrl+K anywhere (fields
 * too); `?`, `N`, `/` and `G` then a letter only outside fields
 * (`notEditing`). None while the terminal owns the keys (keys.ts).
 */
function bindKeys() {
  effect(() => {
    if (!desktop() || terminalOwnsKeys()) return;
    keymap.bind("mod+k", () => paletteOpen.set(!untracked(paletteOpen)));
  });
  effect(() => {
    if (!desktop() || terminalOwnsKeys() || paletteOpen() || shortcutsOpen()) return;
    keymap.bind(Object.fromEntries(GLOBAL_KEYSTROKES.map((keystroke) => [keystroke, () => pressed(keystroke)])), { when: "notEditing" });
  });
  // The letters after `G` are bound only while it waits for one.
  effect(() => {
    if (!pendingGo() || terminalOwnsKeys()) return;
    keymap.bind(Object.fromEntries(GO_KEYSTROKES.map((keystroke) => [keystroke, () => pressed(keystroke)])), { when: "notEditing" });
    const timer = setTimeout(() => pendingGo.set(false), GO_SEQUENCE_MS);
    return () => clearTimeout(timer);
  });
}

const POPUP = "mt-24 max-w-[92%] self-center overflow-hidden rounded-xl border border-border bg-popover";
const BACKDROP = "bg-black/30";

function Key(props: { text: string }) {
  return <Text class="rounded border border-border px-1.5 text-xs text-muted-foreground">{props.text}</Text>;
}

/**
 * ⌘K: actions, places, Work and Sessions of the current computer, ranked as
 * the Expo app ranked them (label prefix, label, detail). The arrows wrap,
 * Return runs, Escape clears the query and then closes.
 */
function Palette() {
  const query = signal("");
  const all = computed(() => {
    if (!paletteOpen()) return [];
    const sessions = [...workers()]
      .sort((a, b) => (b.updated_at ?? 0) - (a.updated_at ?? 0))
      .map((worker) => ({ id: worker.id, name: worker.name || worker.id, detail: worker.project || worker.cwd || undefined }));
    const work = workSurface().slips.map((slip) => ({ workId: slip.workId, title: slip.title, statusLabel: slip.statusLabel, sessionId: slip.sessionId }));
    return buildPaletteItems({ sessions, work });
  });
  const shown = computed(() => filterPaletteItems(all(), query()));
  const byId = computed(() => new Map(shown().map((item) => [item.id, item])));
  // Each opening starts empty.
  effect(() => {
    if (paletteOpen()) query.set("");
  });
  const detail = (item: PaletteItem | undefined) => {
    if (!item) return "";
    if (!query().trim()) return item.detail ?? "";
    return item.detail ? `${item.section} · ${item.detail}` : item.section;
  };
  return (
    <Dialog testID="palette" open={paletteOpen()} animation="fade" onOpenChange={({ open }) => paletteOpen.set(open)}
      class={`${POPUP} w-[620px]`} backdropClass={BACKDROP}>
      <Command testID="palette-command" accessibilityLabel="Commands" searchAccessibilityLabel="Search commands" autoFocus filterable={false}
        items={shown().map((item) => ({ id: item.id, label: item.label, group: query().trim() ? null : item.section }))}
        query={query()} onQueryChange={({ query: next }) => query.set(next)}
        onSelect={({ id }) => {
          const item = untracked(byId).get(id);
          if (item) run(item.action);
        }}
        onCancel={() => paletteOpen.set(false)}
        placeholder="Go to, open a Session or Work, or run an action…"
        searchBarClass="flex-row items-center gap-2 border-b border-border px-4"
        searchClass="h-[52px] flex-1 text-base"
        searchPrefix={<Icon name="search" size={18} />}
        listClass="max-h-[420px] p-1.5"
        groupClass="px-3 pb-1 pt-2.5 text-xs text-muted-foreground"
        itemClass="min-h-[38px] flex-row items-center gap-3 rounded-md px-3 focus:bg-accent"
        renderItem={(entry) => (
          <View class="flex-1 flex-row items-center gap-3">
            <Text class="shrink text-sm line-clamp-1">{entry.label}</Text>
            <Text class="flex-1 text-xs text-muted-foreground line-clamp-1">{detail(byId().get(entry.id))}</Text>
            {byId().get(entry.id)?.shortcut ? <Key text={byId().get(entry.id)!.shortcut!} /> : null}
          </View>
        )}
        empty={<Text testID="palette-empty" class="p-4 text-sm text-muted-foreground">{`Nothing matches “${query()}”.`}</Text>}
        footer={<Text class="border-t border-border px-4 py-2 text-xs text-muted-foreground">{`↑↓ to move · Enter to open · Esc to close · ${mod()}+K anywhere`}</Text>} />
    </Dialog>
  );
}

/** `?`: every shortcut. */
function Shortcuts() {
  return (
    <Dialog testID="shortcuts" open={shortcutsOpen()} animation="fade" onOpenChange={({ open }) => shortcutsOpen.set(open)}
      class={`${POPUP} w-[520px]`} backdropClass={BACKDROP}>
      <View class="min-h-[52px] flex-row items-center border-b border-border pl-[18px] pr-2">
        <Text role="header" class="flex-1 text-lg font-semibold">Keyboard shortcuts</Text>
        <Button testID="shortcuts-close" accessibilityLabel="Close" onPress={() => shortcutsOpen.set(false)} class="size-9 items-center justify-center rounded-full hover:bg-accent">
          <Icon name="close" size={18} />
        </Button>
      </View>
      <Scroll keyboardShouldPersistTaps="handled" class="max-h-[460px]" contentClass="p-1.5">
        {desktopShortcutRows(mod()).map((row) => (
          <View class="min-h-[34px] flex-row items-center gap-3 px-3">
            <Text class="flex-1 text-sm">{row.label}</Text>
            <Key text={row.keys} />
          </View>
        ))}
      </Scroll>
    </Dialog>
  );
}

/** The desktop layout's overlays and keys; nothing below 1024 pt. */
export function DesktopOverlays() {
  bindKeys();
  // Leaving the desktop layout closes them.
  effect(() => {
    if (desktop()) return;
    paletteOpen.set(false);
    shortcutsOpen.set(false);
  });
  return (
    <View class="absolute">
      <Palette />
      <Shortcuts />
    </View>
  );
}
