/**
 * What an empty timeline is showing. Loading and syncing mean history may
 * still arrive, so they never read as an empty conversation: only a loaded,
 * available conversation with no rows is "empty".
 */
export type InterfaceTimelinePhase =
  | "content"
  | "hidden"
  | "loading"
  | "error"
  | "unavailable"
  | "empty";

export function interfaceTimelinePhase({
  itemCount,
  loading,
  error,
  suppressed,
  unavailable,
  syncing,
}: {
  itemCount: number;
  loading: boolean;
  error?: string | null;
  suppressed: boolean;
  unavailable: boolean | null;
  syncing: boolean;
}): InterfaceTimelinePhase {
  if (itemCount > 0) return "content";
  if (suppressed) return "hidden";
  if (loading) return "loading";
  if (error) return "error";
  if (syncing) return "loading";
  if (unavailable) return "unavailable";
  return "empty";
}
