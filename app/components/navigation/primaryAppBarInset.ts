import { createContext, useContext, useEffect } from "react";

/**
 * Docked layouts: Brain's Work column sits beside the conversation, so the app
 * bar (title and ⋯) ends where the column starts instead of running over it.
 */
export const PrimaryAppBarTrailingInsetContext = createContext<(inset: number) => void>(() => undefined);

/** Brain's Work column reports its width while it is shown. */
export function usePrimaryAppBarTrailingInset(inset: number) {
  const setInset = useContext(PrimaryAppBarTrailingInsetContext);
  useEffect(() => {
    setInset(inset);
    return () => setInset(0);
  }, [inset, setInset]);
}
