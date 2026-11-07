import type { ScreenRoute } from "./screen";

/** The viewer socket for one agent's display; takeOver holds the agent off while it is open. */
export function displayRoute(worker: string, takeOver: boolean): ScreenRoute {
  return { path: "/display/viewer", purpose: "mewla-display-view", params: takeOver ? { worker, takeover: "1" } : { worker } };
}
