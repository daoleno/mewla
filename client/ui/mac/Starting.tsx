import { Button } from "nsh/base";
import { Text, View } from "nsh";
import { PetSprite } from "../pets/PetSprite";
import { Composer as ChatComposer, createComposerText } from "../chat/Composer";
import { Icon } from "../kit/Icon";
import { current } from "../session";
import { joinLocalDaemon, localProblem, macLightsRow, thisMacChosen } from "./local";

/**
 * First run while Brain wakes (DESIGN-MAC.md §2.4, `m-waking`): the waking cat in the middle and "Waking up". Home's
 * thread shows it while Brain starts; MacStarting while this Mac's daemon has not answered yet.
 */
export function MacWaking(props: { problem?: () => string; onRetry?: () => void }) {
  return (
    <View testID="mac-waking" class="flex-1 items-center justify-center gap-3 p-6">
      <PetSprite testID="mac-starting-cat" state={() => (props.problem?.() ? "offline" : "waking")} size={120} />
      <If when={props.problem?.()} else={<Text class="text-[15px] leading-[20px] text-muted-foreground">Waking up</Text>}>
        <Text class="text-center text-[13px] leading-[18px] text-muted-foreground">{props.problem!()}</Text>
        <Button onPress={() => props.onRetry?.()}><Text class="text-[13px] font-semibold">Try Again</Text></Button>
      </If>
    </View>
  );
}

/**
 * The Mac app before its own daemon answers, as `m-waking` draws the window: "Brain" beside the traffic lights, the
 * waking cat, and the composer reading "Connecting…" (nothing sends yet). Why it has not answered, with Try Again.
 */
export function MacStarting() {
  const text = createComposerText(() => null);
  return (
    <View testID="mac-starting" class="flex-1 bg-background text-foreground">
      <View class="h-[46px] flex-row items-center pr-3" style={{ paddingLeft: macLightsRow() > 0 ? 84 : 18 }}>
        {/* Chosen in the computers list while another computer is current: back to that one. */}
        <If when={thisMacChosen() && current()}><Button testID="mac-starting-back" accessibilityLabel={`Back to ${current()?.name || "the computer"}`} onPress={() => thisMacChosen.set(false)}
            class="mr-1 size-7 items-center justify-center active:opacity-70"><Icon name="chevron-left" size={18} tone="soft" /></Button></If>
        <Text role="header" class="text-[13.5px] leading-[18px] font-semibold">Brain</Text>
      </View>
      <MacWaking problem={localProblem} onRetry={() => void joinLocalDaemon()} />
      <View class="w-full self-center pb-4" style={{ maxWidth: 700 }}>
        <ChatComposer text={text} placeholder="Connecting…" canSend={() => false} disabled={() => true} disabledReason={() => "Connecting…"} onSend={() => false}
          running={() => false} stopping={() => false} canStop={() => false} onStop={() => {}} />
      </View>
    </View>
  );
}
