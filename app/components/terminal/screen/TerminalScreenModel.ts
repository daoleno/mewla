import type { ConnectionIssue } from "../../../services/connectionIssue";
import type { Worker, ConnectionState } from "../../../store/workers";
import type { WorkItem } from "../../../store/work";
import type { StoredRecentWorkerOpens } from "../../../services/storage";

export interface MenuAnchorLayout {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface TerminalFallbackPresentation {
  /** A connection issue, shown as an error rather than a wait. */
  failed: boolean;
  busy: boolean;
  title: string;
  detail: string;
  hint: string;
}

export function findLinkedWork(
  byKey: Record<string, WorkItem>,
  serverId: string,
  workerId: string,
): WorkItem | undefined {
  if (!serverId || !workerId) return undefined;

  return Object.values(byKey)
    .filter(
      (current) =>
        current.serverId === serverId &&
        current.frontmatter.worker_session === workerId,
    )
    .sort((left, right) => getWorkStartedAt(right) - getWorkStartedAt(left))[0];
}

export function buildTerminalFallbackPresentation({
  hasTerminalRoute,
  connectionState,
  connectionIssue,
}: {
  hasTerminalRoute: boolean;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
}): TerminalFallbackPresentation {
  return {
    failed: Boolean(connectionIssue),
    busy:
      hasTerminalRoute && connectionState === "connecting" && !connectionIssue,
    title: !hasTerminalRoute
      ? "Terminal unavailable"
      : connectionIssue?.title ||
        (connectionState === "connecting"
          ? "Reconnecting to daemon"
          : "Daemon unavailable"),
    detail: !hasTerminalRoute
      ? "Open this terminal again from the Agents tab."
      : connectionIssue?.detail ||
        (connectionState === "connecting"
          ? "Mewla is reconnecting before reopening this terminal."
          : "Start mewla on that machine, or bring the network or tunnel back."),
    hint: !hasTerminalRoute
      ? "The app kept your route, but the live terminal is not ready yet."
      : connectionIssue?.hint ||
        "This terminal will reopen automatically once the daemon is reachable again.",
  };
}

export function sortTerminalWorkers({
  agents,
  recentWorkerOpens: _recentWorkerOpens,
}: {
  agents: Worker[];
  recentWorkerOpens: StoredRecentWorkerOpens;
}) {
  return [...agents];
}

export function buildMenuPosition(
  anchor: MenuAnchorLayout | null,
  windowWidth: number,
  popoverWidth: number,
): { left: number; top: number } {
  const top = Math.max(12, (anchor?.y ?? 12) + (anchor?.height ?? 38) + 16);
  const preferredLeft =
    (anchor?.x ?? windowWidth - 14) + (anchor?.width ?? 0) - popoverWidth;
  const maxLeft = Math.max(12, windowWidth - popoverWidth - 12);

  return {
    left: clamp(preferredLeft, 12, maxLeft),
    top,
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function getWorkStartedAt(item: WorkItem): number {
  const startedAt = Date.parse(
    item.frontmatter.started || item.frontmatter.created || "",
  );
  return Number.isNaN(startedAt) ? 0 : startedAt;
}
