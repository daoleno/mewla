import { useCallback, useEffect, useMemo, useRef } from "react";
import type { BrainObjective } from "../../store/brain";
import { brainWorkOverlay, type BrainWorkOverlayContent } from "./brainWorkOverlayStore";
import type { BrainWorkSlip, BrainWorkSurface } from "./brainWorkSurface";

const slipKey = (slip: BrainWorkSlip) => JSON.stringify({ ...slip, waitedMs: 0 });

/**
 * Same Work, whatever the clock did: the surface is rebuilt whenever the
 * store moves, and `waitedMs` follows the clock. Only what the list draws
 * decides whether the slips need to render again.
 */
export function sameWorkSurface(a: BrainWorkSurface, b: BrainWorkSurface): boolean {
  if (a === b) return true;
  if (a.slips.length !== b.slips.length) return false;
  if (JSON.stringify(a.counts) !== JSON.stringify(b.counts)) return false;
  return a.slips.every((slip, index) => slipKey(slip) === slipKey(b.slips[index]));
}

/**
 * `next`, with every slip that did not change kept as the same object, so a
 * Work update redraws the slip it touched and the list rows memoise on the rest.
 * The previous surface itself when nothing changed.
 */
export function reuseWorkSlips(previous: BrainWorkSurface | undefined, next: BrainWorkSurface): BrainWorkSurface {
  if (!previous) return next;
  if (sameWorkSurface(previous, next)) return previous;
  const before = new Map(previous.slips.map((slip) => [slip.workId, slip]));
  return {
    ...next,
    slips: next.slips.map((slip) => {
      const was = before.get(slip.workId);
      return was && slipKey(was) === slipKey(slip) ? was : slip;
    }),
  };
}

/** The first value that deep-equals every later one, so an equal-but-new object redraws nothing. */
function useStructural<T>(value: T): T {
  const kept = useRef(value);
  if (kept.current !== value && JSON.stringify(kept.current) !== JSON.stringify(value)) kept.current = value;
  return kept.current;
}

/**
 * The Brain screen hands the root's Work sheet what to draw. `content` null
 * means there is no phone sheet (the wide column shows), which also shuts it.
 * The sheet outlives renders of the screen: the screen only publishes, and
 * what it publishes changes only when what the slips draw changes. A Brain
 * event that rebuilds the screen's callbacks or objective redraws no slip: the
 * published functions are one pair that call the screen's latest.
 */
export function useBrainWorkOverlayContent(content: BrainWorkOverlayContent | null): void {
  const latest = useRef(content);
  latest.current = content;
  const onOpenSlip = useCallback((slip: BrainWorkSlip) => latest.current?.onOpenSlip(slip), []);
  // Each action's press is looked up again when pressed, so no slip holds a stale closure.
  const actionsFor = useCallback<NonNullable<BrainWorkOverlayContent["actionsFor"]>>((slip, placement) => {
    const actions = latest.current?.actionsFor?.(slip, placement) ?? [];
    return actions.map((action) => ({
      ...action,
      onPress: () => latest.current?.actionsFor?.(slip, placement).find((item) => item.key === action.key)?.onPress(),
    }));
  }, []);

  const surfaceRef = useRef<BrainWorkSurface | undefined>(undefined);
  const surface = content?.surface;
  if (!surface) surfaceRef.current = undefined;
  else surfaceRef.current = reuseWorkSlips(surfaceRef.current, surface);
  const objective = useStructural<BrainObjective | undefined>(content?.objective);
  // What the slips draw from their actions (label, primary, busy, disabled), per slip.
  const refresh = surface && content?.actionsFor
    ? JSON.stringify(surface.slips.map((slip) => content.actionsFor!(slip, "slip").map((a) => [a.key, a.label, a.primary, a.busy, a.disabled])))
    : "";

  const chrome = content?.chrome;
  const stableSurface = surfaceRef.current;
  const published = useMemo<BrainWorkOverlayContent | null>(
    () => (chrome && stableSurface ? { surface: stableSurface, objective, chrome, onOpenSlip, actionsFor, refresh } : null),
    [actionsFor, chrome, objective, onOpenSlip, refresh, stableSurface],
  );
  useEffect(() => {
    brainWorkOverlay.publish(published);
  }, [published]);
  useEffect(() => () => brainWorkOverlay.publish(null), []);
}
