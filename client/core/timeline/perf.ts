/**
 * The Expo app's timeline projection perf recorder (timelineProjectionPerf.ts)
 * is a dev tool. Core keeps its types so the projection compiles unchanged,
 * and never records: measuring is always off.
 */

export type TimelineProjectionMode = "full" | "incremental";

export type TimelineProjectionFallbackReason =
  | "no-cache"
  | "length-change"
  | "id-sequence-change"
  | "multiple-dirty"
  | "kind-change"
  | "non-message"
  | "timestamp-change"
  | "seq-change"
  | "unbounded-field-delta"
  | "presence-change"
  | "item-missing"
  | "duplicate-event-id"
  | "ambiguous";

export type TimelineProjectionSample = {
  mode: TimelineProjectionMode;
  durationMs: number;
  eventCount: number;
  itemCount: number;
  dirtyStart?: number;
  dirtyEnd?: number;
  fallbackReason?: TimelineProjectionFallbackReason;
  stableRowReuse?: number;
  stableRowChurn?: number;
};

export type TimelineRenderProjectionSample = {
  mode: "full" | "stable" | "update" | "append" | "prepend";
  durationMs: number;
  sourceItemCount: number;
  renderItemCount: number;
  changedSourceCount: number;
  stableRenderReuse: number;
  stableRenderChurn: number;
};

export function isTimelineProjectionPerfEnabled() {
  return false;
}

export function recordTimelineProjectionSample(_sample: TimelineProjectionSample) {}

export function recordTimelineRenderProjectionSample(
  _sample: TimelineRenderProjectionSample,
) {}
