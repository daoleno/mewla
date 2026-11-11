import { cleanup, effect, Image, lifecycle, reduceMotion, signal, untracked, View } from "nsh";
import { CLIP_TIMING, petTransition, type PetState } from "../../../core/settings/petPreview";
import type { Pet } from "./pets";

type Shown = { name: keyof typeof CLIP_TIMING; play: number };

/**
 * Brain as a pet, playing its state's animated clip; moving between the seal and its
 * feet plays the hop out or back in first. Without motion (the system asks for less, or
 * the app is not in front) it shows the pet's still portrait.
 */
export function PetSprite(props: { pet: () => Pet; state: () => PetState; size: number; testID?: string }) {
  const shown = signal<Shown>({ name: clipOf(untracked(props.state)), play: 0 });
  let settled = untracked(props.state);
  let timer: ReturnType<typeof setTimeout> | null = null;
  const stop = () => {
    if (timer) clearTimeout(timer);
    timer = null;
  };
  cleanup(stop);
  const moving = () => !reduceMotion() && lifecycle() === "active";

  effect(() => {
    const state = props.state();
    const live = moving();
    props.pet();
    untracked(() => {
      const from = settled;
      settled = state;
      stop();
      const show = (name: Shown["name"]) => shown.set({ name, play: shown().play + 1 });
      const hop = live ? petTransition(from, state) : null;
      if (!hop || hop === "homeless" || !(hop in CLIP_TIMING)) return show(clipOf(state));
      show(hop as Shown["name"]);
      timer = setTimeout(() => {
        timer = null;
        show(clipOf(settled));
      }, CLIP_TIMING[hop as Shown["name"]].durationMs);
    });
  });

  return (
    <View testID={props.testID} style={{ width: props.size, height: props.size, overflow: "visible" }} accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
      {() => {
        const now = shown();
        const pet = props.pet();
        if (!moving()) return <Image testID="pet-still" src={pet.portrait} style={{ width: props.size, height: props.size }} />;
        const bleed = (now.name === "waking" || now.name === "going_back" ? pet.hopBleed : CLIP_TIMING[now.name].bleed ?? 0) * props.size;
        // A new element each play, so a one-shot (delivered, a hop) starts from its first frame.
        return (
          <Image
            testID={`pet-clip-${now.name}`}
            src={pet.clips[now.name]}
            style={bleed ? { position: "absolute", left: -bleed, top: -bleed, width: props.size + 2 * bleed, height: props.size + 2 * bleed } : { width: props.size, height: props.size }}
          />
        );
      }}
    </View>
  );
}

/** The clip a state plays in the preview (it only plays the states it runs through). */
function clipOf(state: PetState): Shown["name"] {
  return state in CLIP_TIMING ? (state as Shown["name"]) : "idle";
}
