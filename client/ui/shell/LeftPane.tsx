import { computed, effect, keymap, PanResponder, Text, untracked, View, type ViewRef } from "nsh";
import { Button } from "nsh/base";
import { brainRowLine, paneSelection, paneStateLine, paneTrayTitle } from "../../core/tray/pane";
import { PANE_WIDTH, stepPaneWidth } from "../../core/web/places";
import { currentDiagnosis } from "../computer/computers";
import { HomeCat } from "../home/Cat";
import { openBrainHost, showRequestCard } from "../home/Home";
import { Icon } from "../kit/Icon";
import { newSessionOpen } from "../screens/NewSession";
import { brain, brainThread, current } from "../session";
import { StateDotMark } from "../tray/StateControl";
import { refreshOpened, tray } from "../tray/state";
import { SelectedWell, TrayBody, type TrayOpeners } from "../tray/Tray";
import { currentBrain } from "../brain";
import { paletteOpen } from "./desktop/Overlays";
import { paneWidth, setPaneWidth } from "./layout";
import { goHome, nav, openPage } from "./nav";

/** A left-pane row replaces the right pane's page above Brain (SPEC §14.1): Brain › that page › what it pushed. */
const REPLACE: TrayOpeners = {
  session: (workerId) => openPage({ name: "session", workerId }),
  work: (workId) => openPage({ name: "workDetail", workId }),
  item: (id) => openPage({ name: "item", id }),
  calendar: () => openPage({ name: "calendar" }),
  allSessions: () => openPage({ name: "allSessions" }),
  computer: () => openPage({ name: "computer" }),
};

/** Which row's page is in the right pane. */
const selected = computed(() => paneSelection(nav.routes() as readonly { name: string }[]));

/** The pane header (68): the cat (the computer page), the computer's name and its state line. */
function PaneHeader() {
  const line = () => paneStateLine({ kind: tray().state.kind, diagnosis: currentDiagnosis() || undefined });
  return (
    <View testID="pane-header" class="h-[68px] flex-row items-center gap-2 px-2">
      <View class="overflow-hidden rounded-full">
        {selected() === "computer" ? <SelectedWell /> : null}
        <HomeCat onPress={REPLACE.computer} />
      </View>
      <View class="min-w-0 flex-1">
        <Text testID="pane-computer" role="header" class="text-title font-bold tracking-title line-clamp-1">{current()?.name || "Mewla"}</Text>
        {line().words ? (
          <View class="flex-row items-center gap-1.5">
            <StateDotMark dot={line().dot} />
            <Text testID="pane-state" class={`text-sub line-clamp-1 ${line().ink ? "font-semibold text-foreground" : "text-muted-foreground"}`}>{line().words}</Text>
          </View>
        ) : null}
      </View>
    </View>
  );
}

/** "Search or run" (44, surface, radius 14) with the palette's key cap. */
function SearchOrRun() {
  return (
    <Button testID="pane-search" accessibilityLabel="Search or run" onPress={() => paletteOpen.set(true)}
      class="mx-4 mb-2 h-[44px] flex-row items-center gap-2 rounded-[14px] bg-card px-3 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
      <Icon name="search" size={18} tone="soft" />
      <Text class="flex-1 text-body text-muted-foreground line-clamp-1">Search or run</Text>
      <Text testID="pane-search-key" class="rounded-md border border-border px-1.5 py-0.5 text-caption text-muted-foreground">{keymap.label("mod+k").replace(/\+/g, " ")}</Text>
    </Button>
  );
}

/** The Brain row: Brain's mark, "Brain", the last thing Brain said; a live dot while its turn runs. It returns the right pane to Brain. */
function BrainRow() {
  const on = () => selected() === "brain";
  const running = () => brainThread().conversation?.activity?.status === "running";
  const line = () => brainRowLine(brainThread().conversation?.events);
  return (
    <View class="mx-4 overflow-hidden rounded-(--radius-group) bg-sheet-surface">
      <Button testID="pane-brain" accessibilityLabel={line() ? `Brain, ${line()}` : "Brain"} selected={on()} onPress={() => goHome()}
        class="min-h-[68px] flex-row items-center gap-3.5 px-4 py-3 hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
        {on() ? <SelectedWell /> : null}
        <View class="size-[28px] items-center justify-center rounded-lg bg-foreground"><Icon name="brain" size={18} tone="canvas" /></View>
        <View class="min-w-0 flex-1">
          <Text class="text-body font-semibold line-clamp-1">Brain</Text>
          {line() ? <Text testID="pane-brain-line" class="text-sub text-muted-foreground line-clamp-1">{line()}</Text> : null}
        </View>
        {running() ? <View testID="pane-brain-live" accessibilityLabel="Working" class="size-1.5 rounded-full bg-foreground" /> : null}
      </Button>
    </View>
  );
}

/**
 * The handle on the pane's right edge: drag to resize (280–420), double-click or double-tap to
 * reset to 320, ← and → in steps of 16 while it has the focus. Pointer and touch alike.
 */
export function PaneHandle() {
  let start = 0;
  let lastTap = 0;
  let node: ViewRef | null = null;
  const drag = PanResponder.create({
    onStartShouldSetPanResponder: () => true,
    onPanResponderGrant: () => {
      start = untracked(paneWidth);
      // Pressed, it has the focus, as a browser gives it to what is clicked: the arrows are its.
      node?.focus();
    },
    onPanResponderMove: (_e, g) => paneWidth.set(Math.round(Math.min(PANE_WIDTH.max, Math.max(PANE_WIDTH.min, start + g.dx)))),
    onPanResponderRelease: (_e, g) => {
      if (Math.abs(g.dx) < 3 && Math.abs(g.dy) < 3) {
        const now = Date.now();
        if (now - lastTap < 400) {
          lastTap = 0;
          return setPaneWidth(PANE_WIDTH.initial);
        }
        lastTap = now;
      }
      setPaneWidth(untracked(paneWidth));
    },
    onPanResponderTerminate: () => setPaneWidth(untracked(paneWidth)),
  });
  const bindArrows = () => keymap.bind({
    left: () => setPaneWidth(stepPaneWidth(untracked(paneWidth), -1)),
    right: () => setPaneWidth(stepPaneWidth(untracked(paneWidth), 1)),
  });
  let unbind: (() => void) | null = null;
  return (
    <View {...drag.panHandlers} ref={(ref) => void (node = ref)} testID="pane-handle" role="adjustable" accessibilityLabel="Resize the left pane" focusable
      accessibilityValue={{ min: PANE_WIDTH.min, max: PANE_WIDTH.max, now: paneWidth() }}
      onPress={(e) => {
        if (e.clickCount === 2) setPaneWidth(PANE_WIDTH.initial);
      }}
      onFocus={() => {
        unbind?.();
        unbind = bindArrows();
      }}
      onBlur={() => {
        unbind?.();
        unbind = null;
      }}
      class="group w-2 items-center bg-background focus-visible:bg-accent" style={{ cursor: "col-resize" }}>
      <View class="h-full w-px bg-border group-hover:bg-ring" />
    </View>
  );
}

/**
 * The wide layout's left pane (soft-final SPEC §14.1): the tray, always open. The pane header,
 * Search or run, the Brain row, then the tray unchanged. A row replaces the right pane's page
 * above Brain; the row whose page is shown has the pressed well.
 */
export function LeftPane() {
  // Sort › Last opened reads when each Session was opened here: the pane never "opens", so read as pages change.
  effect(() => {
    nav.routes();
    untracked(refreshOpened);
  });
  return (
    <View testID="left-pane" accessibilityLabel="Tray" role="navigation" class="bg-background" style={{ width: paneWidth() }}>
      <PaneHeader />
      <SearchOrRun />
      <BrainRow />
      <View class="flex-1">
        <TrayBody pane open={REPLACE} selected={selected} title={() => paneTrayTitle(tray())}
          onShowCard={showRequestCard}
          onPairAgain={() => openPage({ name: "computer" }, { name: "pair" })}
          onNewSession={() => newSessionOpen.set(true)}
          onUseExecutor={(id) => {
            const executor = (brain()?.executors ?? []).find((ref) => ref.id === id);
            if (executor) void currentBrain.hostSwitch.switchTo(executor);
          }}
          onSwitchExecutor={openBrainHost} />
      </View>
    </View>
  );
}
