import { cleanup, effect, Image, type ImageAsset, lifecycle, reduceMotion, signal, untracked, View } from "nsh";
import { clipFrame, PET_STILLS, type PetClipName, type PetStillName } from "../../core/pets/manifest";
import { petTransition, type PetState } from "../../core/settings/petPreview";
import { pet as chosenPet, type Pet } from "./pets";

/** What is drawn: a clip (one play of it) or the state's still. */
type Shown = { kind: "clip"; name: PetClipName; play: number } | { kind: "still"; state: PetState; play: number };

/** How long a clip plays when it plays "once" (SPEC §5: 2 to 3 s): whole passes, at least 2 s; a one-shot once. */
const ONCE_MIN_MS = 2_000;

/**
 * The pet, the one way it is drawn (SPEC §5). A clip on the bleed canvas (the hops, attention,
 * delivered) is drawn larger than the box and centred on it, so the cat keeps its size and rises
 * past the box; nothing here clips it. Moving between the nap and its feet plays the hop first.
 *
 * - `loop`: the state's clip keeps playing (the Pet page, a large cat on an empty page).
 * - `once`: a change of state, the app coming to the front or `delivered` plays the clip once
 *   (2 to 3 s), then the state's still holds (the header cat).
 * - `still`: the state's still only (a picker tile, a pose).
 *
 * Without motion (reduced motion, the app not in front, `canMove` false) it holds the still.
 */
export function PetSprite(props: {
  state: () => PetState;
  size: number;
  pet?: () => Pet;
  mode?: "loop" | "once" | "still";
  /** False holds still: scrolling, typing, the page not shown. */
  canMove?: () => boolean;
  /** Each change plays `delivered` once (a Work came back done). */
  delivered?: () => number;
  testID?: string;
}) {
  const mode = props.mode ?? "loop";
  const current = () => (props.pet ?? chosenPet)();
  const shown = signal<Shown>({ kind: "still", state: untracked(props.state), play: 0 });
  let timer: ReturnType<typeof setTimeout> | null = null;
  const stop = () => {
    if (timer) clearTimeout(timer);
    timer = null;
  };
  cleanup(stop);
  const show = (next: { kind: "clip"; name: PetClipName } | { kind: "still"; state: PetState }) => shown.set({ ...next, play: untracked(shown).play + 1 });

  /** Plays `steps` one after another, then settles: the last state's clip looping, or its still. */
  const run = (steps: PetClipName[], state: PetState) => {
    stop();
    const pet = untracked(current);
    const settle = () => {
      if (mode === "loop" && state !== "homeless") show({ kind: "clip", name: clipOf(state) });
      else show({ kind: "still", state });
    };
    const next = (index: number) => {
      if (index >= steps.length) return settle();
      const name = steps[index]!;
      show({ kind: "clip", name });
      timer = setTimeout(() => {
        timer = null;
        next(index + 1);
      }, playMs(pet, name, mode === "once" && index === steps.length - 1));
    };
    next(0);
  };

  const moving = () => mode !== "still" && !reduceMotion() && lifecycle() === "active" && (props.canMove ? props.canMove() : true);
  let settled = untracked(props.state);
  let wasActive = untracked(lifecycle) === "active";
  let deliveredSeen = untracked(() => props.delivered?.() ?? 0);
  let first = true;

  effect(() => {
    const state = props.state();
    const live = moving();
    const active = lifecycle() === "active";
    const delivered = props.delivered?.() ?? 0;
    current();
    untracked(() => {
      const from = settled;
      settled = state;
      const changed = state !== from;
      const cameBack = active && !wasActive;
      wasActive = active;
      const deliver = delivered !== deliveredSeen;
      deliveredSeen = delivered;
      const opening = first;
      first = false;
      if (!live) {
        stop();
        return show({ kind: "still", state });
      }
      if (state === "homeless") return run([], state);
      if (deliver) {
        const into = petTransition(state, "delivered");
        const back = petTransition("delivered", state);
        return run([...(into ? [into] : []), "delivered", ...(back ? [back] : [])], state);
      }
      // Played once: on opening, a change of state or the app coming back; a hold lifting (scroll, typing) does not.
      if (mode === "once" && !opening && !changed && !cameBack) return;
      const hop = changed ? petTransition(from, state) : null;
      run([...(hop ? [hop] : []), clipOf(state)], state);
    });
  });

  return (
    <View testID={props.testID} style={{ width: props.size, height: props.size, overflow: "visible" }} accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
      {() => {
        const now = shown();
        const pet = current();
        if (now.kind === "still") return <Image testID="pet-still" src={stillOf(pet, now.state)} style={{ width: props.size, height: props.size }} />;
        const spec = pet.clips[now.name];
        const frame = clipFrame(spec, props.size);
        // A new element each play, so a one-shot (delivered, a hop) starts from its first frame.
        return (
          <Image testID={`pet-clip-${now.name}`} src={pet.art.clips[now.name]}
            style={frame.left ? { position: "absolute", left: frame.left, top: frame.top, width: frame.width, height: frame.height } : { width: props.size, height: props.size }} />
        );
      }}
    </View>
  );
}

/** The clip a state plays. */
function clipOf(state: PetState): PetClipName {
  return state === "homeless" ? "idle" : state;
}

/** The still a state holds: its own (cut tight), the empty bed with no computer, else the portrait. */
export function stillOf(pet: Pet, state: PetState): ImageAsset {
  if (state === "homeless") return pet.art.homeless;
  return (PET_STILLS as readonly string[]).includes(state) ? pet.art.stills[state as PetStillName] : pet.art.portrait;
}

/** How long a step plays: a one-shot its length; a loop played "once" whole passes of at least 2 s. */
function playMs(pet: Pet, name: PetClipName, once: boolean): number {
  const spec = pet.clips[name];
  if (!spec.loop || !once) return spec.total_ms;
  return Math.ceil(ONCE_MIN_MS / spec.total_ms) * spec.total_ms;
}
