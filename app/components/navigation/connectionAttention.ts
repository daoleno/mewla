import type { ConnectionIssue } from "../../services/connectionIssue";
import type { ConnectionState } from "../../store/workers";

/**
 * A reconnect that has not landed after this long is no longer a blip: the
 * menu says so, as it does for Offline.
 */
export const CONNECTION_STALL_MS = 10_000;

export type ConnectionAttention = {
  /** The dot on the menu (☰): a diagnosed issue, or the server out of reach. */
  badge: "issue" | "offline" | null;
  /** The menu footer's state word. */
  detail: string;
  /** The menu button's accessibility label. */
  menuLabel: string;
};

/**
 * The one connection signal, shared by the menu dot (phone) and the menu
 * footer line (both layouts). Healthy is quiet, and so is a short reconnect;
 * pages never carry a connection banner of their own.
 */
export function connectionAttention({
  hasServer,
  connection,
  issue,
  stalled,
}: {
  hasServer: boolean;
  connection: ConnectionState;
  issue: ConnectionIssue | null;
  /** Connecting for at least CONNECTION_STALL_MS. */
  stalled: boolean;
}): ConnectionAttention {
  if (!hasServer) {
    return {
      badge: null,
      detail: "Pair a computer in Settings",
      menuLabel: "Open navigation drawer, no server",
    };
  }
  if (issue) {
    return {
      badge: "issue",
      detail: issue.title,
      menuLabel: `Open navigation drawer, ${issue.title}`,
    };
  }
  if (connection === "offline") {
    return {
      badge: "offline",
      detail: "Offline",
      menuLabel: "Open navigation drawer, server offline",
    };
  }
  if (connection === "connecting") {
    return stalled
      ? {
          badge: "offline",
          detail: "Reconnecting",
          menuLabel: "Open navigation drawer, reconnecting to the server",
        }
      : { badge: null, detail: "Connecting", menuLabel: "Open navigation drawer" };
  }
  return { badge: null, detail: "Connected", menuLabel: "Open navigation drawer" };
}
