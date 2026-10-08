/**
 * Worker IDs are tmux pane identities (for example, `%152`). A raw pane ID
 * is not safe as an Expo Router path segment because native linking parses `%`
 * as URI-escape syntax before the route receives the parameter. Keep the
 * route token separate from the Worker identity and use a percent-free UTF-8
 * hex token so Router serialization cannot decode it before the screen does.
 */
export function encodeWorkerRouteId(workerId: string): string {
  const bytes = encodeURIComponent(workerId).replace(
    /%([0-9a-f]{2})/gi,
    (_, hex: string) => String.fromCharCode(Number.parseInt(hex, 16)),
  );
  return `w${Array.from(bytes, (char) =>
    char.charCodeAt(0).toString(16).padStart(2, "0"),
  ).join("")}`;
}

export function decodeWorkerRouteId(routeId: string): string {
  if (!/^w(?:[0-9a-f]{2})+$/i.test(routeId)) {
    throw new Error("Invalid Worker route ID");
  }
  const bytes = routeId
    .slice(1)
    .match(/../g)!
    .map((hex) => `%${hex}`)
    .join("");
  return decodeURIComponent(bytes);
}

export type TerminalRouteParams = Record<string, string | undefined> & {
  id: string;
  serverId: string;
};

/** Build the only supported route shape for a Worker-owned Terminal screen. */
export function terminalRouteParams(
  workerId: string,
  serverId: string,
  extra: Record<string, string | undefined> = {},
): TerminalRouteParams {
  return {
    ...extra,
    id: encodeWorkerRouteId(workerId),
    serverId,
  };
}

/**
 * Resolve a Terminal link opened directly (a typed, shared or reloaded URL).
 * A link without a server names the current server. Returns the canonical
 * params, or null when the link names another server or no Worker route.
 */
export function resolveTerminalLink(
  routeId: string | undefined,
  serverId: string | undefined,
  currentServerId: string | null,
): TerminalRouteParams | null {
  if (!routeId || !currentServerId) return null;
  if (serverId && serverId !== currentServerId) return null;
  try {
    return terminalRouteParams(decodeWorkerRouteId(routeId), currentServerId);
  } catch {
    // Raw pane IDs are not routes: the URL decodes their % before we see it.
    return null;
  }
}
