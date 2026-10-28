import { createContext, useContext, useEffect } from "react";
import type { PrimaryRouteName } from "../../services/interactionTrace";

/**
 * How a docked page sits under the app bar. `trailingInset` is a side column
 * the bar ends before (Brain's Work column).
 */
export interface PrimaryAppBarLayout {
  trailingInset?: number;
}

export const PrimaryAppBarLayoutContext = createContext<
  (route: PrimaryRouteName, layout: PrimaryAppBarLayout | null) => void
>(() => undefined);

/** A primary page reports its layout while it is mounted. */
export function usePrimaryAppBarLayout(
  route: PrimaryRouteName,
  { trailingInset }: PrimaryAppBarLayout,
) {
  const setLayout = useContext(PrimaryAppBarLayoutContext);
  useEffect(() => {
    setLayout(route, { trailingInset });
    return () => setLayout(route, null);
  }, [route, setLayout, trailingInset]);
}
