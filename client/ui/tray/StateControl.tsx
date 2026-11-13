import { computed, Text, View } from "nsh";
import { stateShortWords, type StateDot } from "../../core/tray/model";
import { Icon } from "../kit/Icon";
import { trayOpen } from "../shell/nav";
import { refreshOpened, tray } from "./state";

/**
 * The state's 8 pt mark: red waits for you, ink runs, a soft ring is on its way or away (SPEC §11 State control).
 * Read in the JSX: a component runs once, so a mark chosen at the top stayed the first state's (a ring while
 * connecting, kept beside "1 running").
 */
export function StateDotMark(props: { dot: StateDot }) {
  return <>{props.dot === "seal" ? <View testID="state-dot-seal" class="size-2 rounded-full bg-seal" />
    : props.dot === "ink" ? <View testID="state-dot-ink" class="size-2 rounded-full bg-foreground" />
    : props.dot === "ring" ? <View testID="state-dot-ring" class="size-2 rounded-full border-[1.5px] border-muted-foreground" />
    : null}</>;
}

/**
 * The header's state control: one pill saying how the computer and its work stand ("All quiet",
 * "3 running", "2 need you", "Offline"…). A tap opens or closes the tray; "Pair again" goes
 * straight to pairing. 36 high, its hit area 44.
 */
export function StateControl(props: {
  onPairAgain: () => void;
  /**
   * In a capsule header (SPEC §11): no pill and no chevron, the capsule is its surface; 44 high, 15/600, the
   * dot 7 before the words. `short`: collapsed, need you and running give way to their dot and number ("● 3").
   */
  capsule?: { short: () => boolean };
}) {
  const state = () => tray().state;
  const press = () => {
    if (state().kind === "pair") return props.onPairAgain();
    if (!trayOpen()) refreshOpened();
    trayOpen.set(!trayOpen());
  };
  // No computer, no state control. Read in the JSX (a component runs once); a computed so the pill is made once.
  const hidden = computed(() => state().kind === "none");
  return <>{hidden() ? null : (
    <View testID="state-control" role="button" accessible onPress={press} accessibilityLabel={state().words}
      accessibilityHint={state().kind === "pair" ? "Pairs this phone again" : trayOpen() ? "Closes the tray" : "Opens the tray"}
      accessibilityState={{ expanded: state().kind === "pair" ? undefined : trayOpen() }}
      class={props.capsule ? "min-h-[44px] min-w-[44px] shrink-0 justify-center rounded-full active:bg-accent" : "min-h-[44px] shrink justify-center active:opacity-70"}>
      {props.capsule ? (
        <View class="h-[44px] flex-row items-center justify-center pl-2 pr-2.5" style={{ gap: 7 }}>
          <StateDotMark dot={state().dot} />
          <Text testID="state-words" class={`text-[15px] font-semibold ${state().ink ? "text-foreground" : "text-muted-foreground"}`}>
            {props.capsule.short() ? stateShortWords(state()) : state().words}
          </Text>
        </View>
      ) : (
        <View class="h-[36px] flex-row items-center gap-1.5 rounded-full bg-card pl-[13px] pr-[9px]">
          <StateDotMark dot={state().dot} />
          <Text testID="state-words" class={`text-sub font-semibold line-clamp-1 ${state().ink ? "text-foreground" : "text-muted-foreground"}`}>{state().words}</Text>
          {state().chevron ? <Icon name={trayOpen() ? "chevron-up" : "chevron-down"} size={16} tone="faint" /> : <View class="w-1" />}
        </View>
      )}
    </View>
  )}</>;
}
