import { cleanup, computed, effect, Image, lifecycle, reduceMotion, signal, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { pet } from "../more/pet/pets";
import { petClip, type PetClip } from "../pets/clips";
import { brainThread } from "../session";
import { tray } from "../tray/state";
import { catNeedsYou } from "../computer/ComputerPage";

/** How long a state's clip plays before the cat holds still (SPEC §5: 2–3 s). */
const CLIP_MS = 2600;

/**
 * The cat is the computer (SPEC §1.3), from the same model as the state control: quiet it naps;
 * busy it works (Brain's turn) or delegates (Workers); waiting on you it calls; connecting or
 * starting it wakes; offline, unpaired or unable to start it sits in the grey ring.
 */
const state = computed<PetClip>(() => {
  switch (tray().state.kind) {
    case "needs": return "attention";
    case "running": return brainThread().conversation?.activity?.status === "running" ? "working" : "delegating";
    case "quiet": return "idle";
    case "connecting":
    case "starting": return "waking";
    case "none": return "homeless";
    default: return "offline";
  }
});

/**
 * The header's cat: a 40 pt cat in a 48 pt round target that opens the computer page, a red dot
 * when something behind it needs you. A clip plays once when the state changes and when the app
 * comes back, then the cat holds still; with less motion asked for it only holds still. The still
 * is the pet's portrait until each state has its own (art task).
 */
export function HomeCat(props: { onPress: () => void }) {
  const playing = signal(true);
  let timer: ReturnType<typeof setTimeout> | null = null;
  const stop = () => {
    if (timer) clearTimeout(timer);
    timer = null;
  };
  cleanup(stop);
  effect(() => {
    state();
    pet();
    if (lifecycle() !== "active" || reduceMotion()) {
      stop();
      return playing.set(false);
    }
    untracked(() => {
      stop();
      playing.set(true);
      timer = setTimeout(() => {
        timer = null;
        playing.set(false);
      }, CLIP_MS);
    });
  });
  return (
    <Button testID="home-cat" accessibilityLabel="Computer" onPress={props.onPress} class="size-12 items-center justify-center rounded-full active:bg-accent">
      <Image src={playing() ? petClip(state()) : pet().portrait} accessibilityElementsHidden style={{ width: 40, height: 40 }} />
      {catNeedsYou() ? <View testID="home-cat-dot" accessibilityLabel="Needs you" class="absolute right-1 top-1 size-[14px] rounded-full border-2 border-background bg-seal" /> : null}
    </Button>
  );
}
