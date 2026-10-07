/**
 * Seal & Slip Work states. Each state owns one glyph shape, so the six stay
 * apart in greyscale and for colour-blind readers; colour only reinforces it.
 * "Needs you" is the seal (vermilion) and is the one loud state.
 */
export type WorkStatus =
  | "ready"
  | "running"
  | "needs"
  | "warning"
  | "failed"
  | "blocked";

export type WorkStatusGlyph =
  | "check-disc"
  | "arc"
  | "seal-dot"
  | "triangle"
  | "crossed-box"
  | "dashed-ring";

export const WORK_STATUS_GLYPHS: Readonly<Record<WorkStatus, WorkStatusGlyph>> = {
  ready: "check-disc",
  running: "arc",
  needs: "seal-dot",
  warning: "triangle",
  failed: "crossed-box",
  blocked: "dashed-ring",
};

export const WORK_STATUS_LABELS: Readonly<Record<WorkStatus, string>> = {
  ready: "Ready",
  running: "Running",
  needs: "Needs you",
  warning: "Warning",
  failed: "Failed",
  blocked: "Blocked",
};

export interface WorkStatusInks {
  statusReady: string;
  statusRunning: string;
  seal: string;
  sealText: string;
  statusWarning: string;
  statusFailed: string;
  statusBlocked: string;
}

/** The glyph colour for a state. */
export function workStatusInk(status: WorkStatus, inks: WorkStatusInks): string {
  switch (status) {
    case "ready":
      return inks.statusReady;
    case "running":
      return inks.statusRunning;
    case "needs":
      return inks.seal;
    case "warning":
      return inks.statusWarning;
    case "failed":
      return inks.statusFailed;
    case "blocked":
      return inks.statusBlocked;
  }
}

/** The status word's colour; "Needs you" as words uses the readable seal. */
export function workStatusTextInk(status: WorkStatus, inks: WorkStatusInks): string {
  return status === "needs" ? inks.sealText : workStatusInk(status, inks);
}
