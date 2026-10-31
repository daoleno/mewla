import { AppState, type AppStateStatus } from "react-native";

const KNOWN: ReadonlySet<string> = new Set(["active", "background", "inactive", "extension", "unknown"]);

/** React Native types `AppState.currentState` as a bare string; this narrows it to the status union. */
export function currentAppState(): AppStateStatus {
  const state = AppState.currentState;
  return state && KNOWN.has(state) ? (state as AppStateStatus) : "unknown";
}
