import type { ScreenRoute } from "./screen";

/** The viewer socket for one agent's display. What the person does reaches it and is told to the agent. */
export function displayRoute(worker: string): ScreenRoute {
  return { path: "/display/viewer", purpose: "mewla-display-view", params: { worker } };
}
