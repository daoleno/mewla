import { computed, effect, keymap, PanResponder, Text, untracked, View, type ViewRef } from "nsh";
import { Button } from "nsh/base";
import { brainRowLine, paneSelection, paneStateLine, paneTrayTitle } from "../../core/tray/pane";
import { stepPaneWidth } from "../../core/web/places";
import { currentDiagnosis } from "../computer/computers";
import { HomeCat } from "../home/Cat";
import { openBrainHost, showRequestCard } from "../home/Home";
import { Icon } from "../kit/Icon";
import { newSessionOpen } from "../screens/NewSession";
import { brain, brainThread, current } from "../session";
import { StateDotMark } from "../tray/StateControl";
import { refreshOpened, tray } from "../tray/state";
import { homeHeaderInset, SelectedWell, TrayBody, type TrayOpeners } from "../tray/Tray";
import { currentBrain } from "../brain";
import { isMacApp, isThisMac, localProblem, macLightsRow } from "../mac/local";
import { paletteOpen } from "./desktop/Overlays";
import { paneRange, paneWidth, setPaneWidth } from "./layout";
import { goHome, nav, openPage } from "./nav";
import { MaterialTint } from "../mac/materials";

/** A left-pane row replaces the right pane's page above Brain (SPEC §14.1): Brain › that page › what it pushed. */
const REPLACE: TrayOpeners = {
  session: (workerId) => openPage({ name: "session", workerId }),
  work: (workId) => openPage({ name: "workDetail", workId }),
  item: (id) => openPage({ name: "item", id }),
  trigger: (id) => openPage({ name: "trigger", id }),
  calendar: () => openPage({ name: "calendar" }),
  allSessions: () => openPage({ name: "allSessions" }),
  computer: () => openPage({ name: "computer" }),
};

/** Mewla for Mac's density (DESIGN-MAC.md §2.3): the pane's parts at the Mac's sizes, 10 pt from its sides. */
const MAC = isMacApp();

/** Which row's page is in the right pane. */
const selected = computed(() => paneSelection(nav.routes() as readonly { name: string }[]));

/** The pane header (68): the cat (the computer page), the computer's name and its state line. */
function PaneHeader() {
  const line = () => paneStateLine({ kind: tray().state.kind, diagnosis: currentDiagnosis() || undefined });
  return (
    <View testID="pane-header" class={MAC ? "mb-2 h-[48px] flex-row items-center gap-2 px-2.5" : "h-[68px] flex-row items-center gap-2 px-2"}>
      <View class="overflow-hidden rounded-full">
        <If when={selected() === "computer"}><SelectedWell /></If>
        <HomeCat onPress={REPLACE.computer} size={MAC ? 40 : undefined} box={MAC ? 44 : undefined} />
      </View>
      <View class="min-w-0 flex-1">
        <Text testID="pane-computer" role="header" class={MAC ? "text-[15px] leading-[19px] font-bold line-clamp-1" : "text-title font-bold tracking-title line-clamp-1"}>{current()?.name || "Mewla"}</Text>
        <If when={line().words}><View class="flex-row items-center gap-1.5">
            {/* On the Mac, online is the 6 pt `ok` dot (win.css `.lp .who span i`); the other states keep theirs. */}
            <If when={MAC && line().dot === "ink"} else={<StateDotMark dot={line().dot} small={MAC} />}><View testID="state-dot-ok" class="size-1.5 rounded-full bg-ok" /></If>
            {/* On the Mac it is this Mac: "This Mac · Online", 12 (RESULT-DESIGN-PAIRS #6). */}
            <Text testID="pane-state" class={`${MAC ? "text-[12px] leading-[16px]" : "text-sub"} line-clamp-1 ${line().ink ? "font-semibold text-foreground" : "text-muted-foreground"}`}>{MAC && isThisMac(current()) ? `This Mac · ${localProblem() || line().words}` : line().words}</Text>
          </View></If>
      </View>
    </View>
  );
}

/** "Search or run" (44, surface, radius 14) with the palette's key cap. */
function SearchOrRun() {
  return (
    <Button testID="pane-search" accessibilityLabel="Search or run" onPress={() => paletteOpen.set(true)}
      class={`${MAC ? "mx-2.5 mb-2 h-[30px] rounded-[9px] bg-card/70 px-2.5" : "mx-4 mb-2 h-[44px] rounded-[14px] bg-card px-3"} flex-row items-center gap-2 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}>
      <Icon name="search" size={MAC ? 15 : 18} tone="soft" />
      <Text class={`flex-1 text-muted-foreground line-clamp-1 ${MAC ? "text-[13px]" : "text-body"}`}>Search or run</Text>
      <View class="rounded-md border border-border px-1.5 py-0.5"><Text testID="pane-search-key" class={MAC ? "text-[11px] text-muted-foreground" : "text-caption text-muted-foreground"}>{keymap.label("mod+k").replace(/\+/g, " ")}</Text></View>
    </Button>
  );
}

/** The Brain row: Brain's mark, "Brain", the last thing Brain said; a live dot while its turn runs. It returns the right pane to Brain. */
function BrainRow() {
  // Brain's thread, or its host's own Session (Brain's terminal): w-session selects the row of the Session in front.
  const on = () => selected() === "brain" || (Boolean(brain()?.host_worker?.id) && selected() === `session:${brain()?.host_worker?.id}`);
  const running = () => brainThread().conversation?.activity?.status === "running";
  const line = () => brainRowLine(brainThread().conversation?.events);
  return (
    <View class={MAC ? "mx-2.5 overflow-hidden rounded-[10px]" : "mx-4 overflow-hidden rounded-(--radius-group) bg-sheet-surface"}>
      <Button testID="pane-brain" accessibilityLabel={line() ? `Brain, ${line()}` : "Brain"} selected={on()} onPress={() => goHome()}
        class={`${MAC ? "min-h-[44px] gap-2.5 px-2.5 py-1.5" : "min-h-[68px] gap-3.5 px-4 py-3"} flex-row items-center hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`}>
        <If when={on()}><SelectedWell /></If>
        <View class={`${MAC ? "size-[26px] rounded-[7px]" : "size-[28px] rounded-lg"} items-center justify-center bg-foreground`}><Icon name="brain" size={MAC ? 16 : 18} tone="canvas" /></View>
        <View class="min-w-0 flex-1">
          <Text class={MAC ? "text-[13.5px] font-semibold line-clamp-1" : "text-body font-semibold line-clamp-1"}>Brain</Text>
          <If when={line()}><Text testID="pane-brain-line" class={MAC ? "text-[12px] text-muted-foreground line-clamp-1" : "text-sub text-muted-foreground line-clamp-1"}>{line()}</Text></If>
        </View>
        <If when={running()}><View testID="pane-brain-live" accessibilityLabel="Working" class="size-1.5 rounded-full bg-foreground" /></If>
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
    onPanResponderMove: (_e, g) => paneWidth.set(Math.round(Math.min(paneRange.max, Math.max(paneRange.min, start + g.dx)))),
    onPanResponderRelease: (_e, g) => {
      if (Math.abs(g.dx) < 3 && Math.abs(g.dy) < 3) {
        const now = Date.now();
        if (now - lastTap < 400) {
          lastTap = 0;
          return setPaneWidth(paneRange.initial);
        }
        lastTap = now;
      }
      setPaneWidth(untracked(paneWidth));
    },
    onPanResponderTerminate: () => setPaneWidth(untracked(paneWidth)),
  });
  const bindArrows = () => keymap.bind({
    left: () => setPaneWidth(stepPaneWidth(untracked(paneWidth), -1, paneRange)),
    right: () => setPaneWidth(stepPaneWidth(untracked(paneWidth), 1, paneRange)),
  });
  let unbind: (() => void) | null = null;
  return (
    <View {...drag.panHandlers} ref={(ref) => void (node = ref)} testID="pane-handle" role="adjustable" accessibilityLabel="Resize the left pane" focusable
      accessibilityValue={{ min: paneRange.min, max: paneRange.max, now: paneWidth() }}
      onPress={(e) => {
        if (e.clickCount === 2) setPaneWidth(paneRange.initial);
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
    // Below the status bar on a touch screen (an Android foldable, a tablet), as Brain's header beside it is; on the
    // Mac below the traffic lights' row.
    // On the Mac the sidebar material, the desktop showing through (P2), with a hairline at its right.
    <View testID="left-pane" accessibilityLabel="Tray" role="navigation" class={isMacApp() ? "border-r-[0.5px] border-border" : "bg-background"}
      macos={isMacApp() ? { backdrop: { material: "sidebar", blending: "behindWindow" } } : undefined}
      style={{ width: paneWidth(), paddingTop: homeHeaderInset() + macLightsRow() }}>
      <If when={isMacApp()}><MaterialTint material="sidebar" /></If>
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
