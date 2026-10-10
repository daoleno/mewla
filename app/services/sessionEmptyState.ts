export function sessionEmptyState(hasServer: boolean, connection: string | undefined, filtered = false) {
  if (!hasServer) return { title: "Connect your computer", icon: "server", action: "pair", label: "Pair a computer", busy: false } as const;
  if (connection === "connecting") return { title: "Connecting", icon: "server", action: null, label: "", busy: true } as const;
  if (connection !== "connected") return { title: "Server offline", icon: "cloud-offline", action: "retry", label: "Retry connection", busy: false } as const;
  if (filtered) return { title: "No matches", icon: "search", action: "clear", label: "Clear filters", busy: false } as const;
  return { title: "No sessions yet", icon: "terminal", action: "terminal", label: "New session", busy: false } as const;
}

/**
 * The cat for the Sessions empty state: the empty bed with no computer,
 * waking while it connects, the grey seal offline, asleep with nothing open.
 */
export function sessionEmptyCat(
  action: "pair" | "retry" | "clear" | "terminal" | null,
  busy: boolean,
): "homeless" | "waking" | "offline" | "idle" | null {
  if (busy) return "waking";
  if (action === "pair") return "homeless";
  if (action === "retry") return "offline";
  // A filter that matches nothing is about the filter, not about Brain.
  if (action === "clear") return null;
  return "idle";
}
