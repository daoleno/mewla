/**
 * The pet acting out Brain's states (Settings → Pet), as the Expo app played it: one
 * animated clip per state, and the hop out of the seal or back into it in between.
 */

export type PetState = "homeless" | "offline" | "waking" | "idle" | "working" | "delegating" | "attention" | "delivered";
export type PetClipName = PetState | "going_back";

/** The states the preview runs through, one every 3.2 s, so the pet shows its range. */
export const PREVIEW_STATES: readonly PetState[] = ["delegating", "working", "delivered", "idle"];
export const PREVIEW_STEP_MS = 3_200;

export interface PetClipTiming {
  loop: boolean;
  /** One pass through every frame. */
  durationMs: number;
  /** How far the clip draws past the pet's box on each side, as a share of the box (a hop's ring). */
  bleed?: number;
}

/** Each clip's timing, the same for every pack (one pack's hops bleed wider). */
export const CLIP_TIMING: Record<"idle" | "working" | "delegating" | "delivered" | "waking" | "going_back", PetClipTiming> = {
  idle: { loop: true, durationMs: 2120 },
  working: { loop: true, durationMs: 820 },
  delegating: { loop: true, durationMs: 1100 },
  delivered: { loop: false, durationMs: 840 },
  waking: { loop: false, durationMs: 1440, bleed: 0.2812 },
  going_back: { loop: false, durationMs: 1370, bleed: 0.2812 },
};

const IN_SEAL = new Set<PetState>(["homeless", "offline", "idle"]);

/** Where a state leaves the pet: in the seal (asleep, greyed or gone), or out on its feet. */
export function petInSeal(state: PetState): boolean {
  return IN_SEAL.has(state);
}

/**
 * The clip to play on the way from one state to another before the new state's own:
 * going_back on the way into the seal, waking on the way out; null on the same side.
 */
export function petTransition(from: PetState, to: PetState): PetClipName | null {
  if (from === to || to === "waking" || from === "homeless") return null;
  const fromSeal = petInSeal(from);
  const toSeal = petInSeal(to);
  if (fromSeal && !toSeal) return "waking";
  if (!fromSeal && toSeal) return "going_back";
  return null;
}

/** The preview's state at a step (it wraps around). */
export function previewState(step: number): PetState {
  return PREVIEW_STATES[((step % PREVIEW_STATES.length) + PREVIEW_STATES.length) % PREVIEW_STATES.length];
}
