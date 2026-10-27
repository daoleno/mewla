import { createContext, useContext, useEffect } from "react";
import type { PrimaryRouteName } from "../../services/interactionTrace";

/**
 * How a docked page's content sits under the app bar, so the bar's title
 * and ⋯ line up with it:
 * - `trailingInset`: a side column the bar ends before (Brain's Work column).
 * - `column` and `columnInset`: a centered content column whose text starts
 *   `columnInset` in (the Sessions list); the title sits on that edge.
 */
export interface PrimaryAppBarLayout {
  trailingInset?: number;
  column?: number;
  columnInset?: number;
}

export const PrimaryAppBarLayoutContext = createContext<
  (route: PrimaryRouteName, layout: PrimaryAppBarLayout | null) => void
>(() => undefined);

/** A primary page reports its layout while it is mounted. */
export function usePrimaryAppBarLayout(
  route: PrimaryRouteName,
  { trailingInset, column, columnInset }: PrimaryAppBarLayout,
) {
  const setLayout = useContext(PrimaryAppBarLayoutContext);
  useEffect(() => {
    setLayout(route, { trailingInset, column, columnInset });
    return () => setLayout(route, null);
  }, [column, columnInset, route, setLayout, trailingInset]);
}
