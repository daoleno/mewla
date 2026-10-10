import { useEffect, useReducer, useState } from "react";
import { loadingVeilPhase, type LoadingVeilPhase } from "./loadingVeil";

type Span = { startedAt: number; endedAt: number | null };

/**
 * Where a loading screen is in its life (see loadingVeilPhase). Loading
 * that resumes while the screen is still up keeps the same screen.
 */
export function useLoadingVeil(loading: boolean): LoadingVeilPhase {
  const [span, setSpan] = useState<Span | null>(() => (loading ? { startedAt: Date.now(), endedAt: null } : null));
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
  const state = span ? loadingVeilPhase({ ...span, now: Date.now() }) : { phase: "gone" as const };
  const nextAt = state.nextAt;
  useEffect(() => {
    if (nextAt === undefined) return;
    const timer = setTimeout(tick, Math.max(0, nextAt - Date.now()));
    return () => clearTimeout(timer);
  }, [nextAt]);
  return state.phase;
}
