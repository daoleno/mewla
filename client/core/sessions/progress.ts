/**
 * A Worker's progress bar (SPEC §11 tray row: five 4 pt segments) from the phase it reports. The daemon
 * accepts six phases (`daemon/classifier/progress.go` validProgressPhase): starting shows an empty bar,
 * then one segment each for reading, planning, working, verifying and reporting. A Session that reports
 * no phase (a plain terminal, an agent that never reported) has no bar.
 */
export const PROGRESS_SEGMENTS = 5;

const FILLED: Record<string, number> = { starting: 0, reading: 1, planning: 2, working: 3, verifying: 4, reporting: 5 };

/** Segments filled (0–5), or null for no bar. */
export function progressSteps(phase: string | undefined | null): number | null {
  const filled = phase ? FILLED[phase.trim().toLowerCase()] : undefined;
  return filled === undefined ? null : filled;
}
