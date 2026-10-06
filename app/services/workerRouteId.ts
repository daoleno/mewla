/**
 * Worker IDs are tmux pane identities (for example, `%152`). A raw pane ID
 * is not safe as an Expo Router path segment because native linking parses `%`
 * as URI-escape syntax before the route receives the parameter. Keep the
 * route token separate from the Worker identity and decode it exactly once at
 * the terminal route boundary.
 */
export function encodeWorkerRouteId(workerId: string): string {
  return encodeURIComponent(workerId);
}

export function decodeWorkerRouteId(routeId: string): string {
  return decodeURIComponent(routeId);
}
