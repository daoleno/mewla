import { computed, Text, View } from "nsh";
import { stateShortWords, type StateDot } from "../../core/tray/model";
import { dense } from "../kit/density";
import { Icon } from "../kit/Icon";
import { trayOpen } from "../shell/nav";
import { refreshOpened, tray } from "./state";

/**
 * The state's 8 pt mark: red waits for you, ink runs, a soft ring is on its way or away (SPEC §11 State control).
 * Read in the JSX: a component runs once, so a mark chosen at the top stayed the first state's (a ring while
 * connecting, kept beside "1 running").
 */
export function StateDotMark(props: { dot: StateDot; /** 7 on a Mac's header (RESULT-DESIGN-PAIRS #10); 8 otherwise. */ small?: boolean }) {
  const size = props.small ? "size-[7px]" : "size-2";
  return <><If when={props.dot === "seal"} else={<If when={props.dot === "ink"} else={<If when={props.dot === "ring"}><View testID="state-dot-ring" class={`${size} rounded-full border-[1.5px] border-muted-foreground`} /></If>}><View testID="state-dot-ink" class={`${size} rounded-full bg-foreground`} /></If>}><View testID="state-dot-seal" class={`${size} rounded-full bg-seal`} /></If></>;
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
  // Mewla for Mac: the capsule's words 13/600, the dot 7 and 6 before them (RESULT-DESIGN-PAIRS #10).
  const size = dense({ phone: { words: "text-[15px]", gap: 7, small: false }, window: { words: "text-[13px] leading-[17px]", gap: 6, small: true } });
  return <><If when={!hidden()}><View testID="state-control" role="button" accessible onPress={press} accessibilityLabel={state().words}
      accessibilityHint={state().kind === "pair" ? "Pairs this phone again" : trayOpen() ? "Closes the tray" : "Opens the tray"}
      accessibilityState={{ expanded: state().kind === "pair" ? undefined : trayOpen() }}
      class={props.capsule ? "min-h-[44px] min-w-[44px] shrink-0 justify-center rounded-full active:bg-accent" : "min-h-[44px] shrink justify-center active:opacity-70"}>
      <If when={props.capsule} else={<View class="h-[36px] flex-row items-center gap-1.5 rounded-full bg-card pl-[13px] pr-[9px]">
          <StateDotMark dot={state().dot} />
          <Text testID="state-words" class={`text-sub font-semibold line-clamp-1 ${state().ink ? "text-foreground" : "text-muted-foreground"}`}>{state().words}</Text>
          <If when={state().chevron} else={<View class="w-1" />}><Icon name={trayOpen() ? "chevron-up" : "chevron-down"} size={16} tone="faint" /></If>
        </View>}>{(capsule) => <View class="h-[44px] flex-row items-center justify-center pl-2 pr-2.5" style={{ gap: size.gap }}>
          <StateDotMark dot={state().dot} small={size.small} />
          <Text testID="state-words" class={`${size.words} font-semibold ${state().ink ? "text-foreground" : "text-muted-foreground"}`}>
            {capsule().short() ? stateShortWords(state()) : state().words}
          </Text>
        </View>}</If>
    </View></If></>;
}
