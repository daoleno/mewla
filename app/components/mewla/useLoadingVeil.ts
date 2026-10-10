import { useEffect, useReducer, useRef, useState } from "react";
import { carriedLoadStart, loadingVeilPhase, type CarriedLoad, type LoadingVeilPhase } from "./loadingVeil";

type Span = { startedAt: number; endedAt: number | null };

/** Loads still running, by continuity key, for a screen that remounts mid-load. */
const carried = new Map<string, CarriedLoad>();

/**
 * Where a loading screen is in its life (see loadingVeilPhase). Loading
 * that resumes while the screen is still up keeps the same screen. With a
 * `continuity` key, a remount mid-load (Brain's chat re-keys when its first
 * snapshot names the thread) continues the same load instead of waiting
 * again.
 */
export function useLoadingVeil(loading: boolean, continuity?: string): LoadingVeilPhase {
  const [span, setSpan] = useState<Span | null>(() => {
    if (!loading) return null;
    const now = Date.now();
    const startedAt = continuity ? carriedLoadStart(carried.get(continuity), now) : null;
    return { startedAt: startedAt ?? now, endedAt: null };
  });
  const [, tick] = useReducer((count: number) => count + 1, 0);
  useEffect(() => {
    setSpan((previous) => {
      const now = Date.now();
      if (!loading) return previous && previous.endedAt === null ? { ...previous, endedAt: now } : previous;
      if (previous?.endedAt === null) return previous;
      const up = previous && loadingVeilPhase({ ...previous, now }).phase !== "gone";
      return up ? { ...previous, endedAt: null } : { startedAt: now, endedAt: null };
    });
  }, [loading]);
  const running = span?.endedAt === null ? span.startedAt : null;
  const runningRef = useRef(running);
  runningRef.current = running;
  useEffect(() => {
    if (!continuity) return;
    if (running === null) carried.delete(continuity);
    else carried.set(continuity, { startedAt: running, leftAt: null });
  }, [continuity, running]);
  useEffect(
    () => () => {
      if (!continuity || runningRef.current === null) return;
      if (carried.get(continuity)?.startedAt === runningRef.current) {
        carried.set(continuity, { startedAt: runningRef.current, leftAt: Date.now() });
      }
    },
    [continuity],
  );
  const state = span ? loadingVeilPhase({ ...span, now: Date.now() }) : { phase: "gone" as const };
  const nextAt = state.nextAt;
  useEffect(() => {
    if (nextAt === undefined) return;
    const timer = setTimeout(tick, Math.max(0, nextAt - Date.now()));
    return () => clearTimeout(timer);
  }, [nextAt]);
  return state.phase;
}
