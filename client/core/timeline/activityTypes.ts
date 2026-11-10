import type { ToolDeveloperDetails } from "./toolCallDetails";
import type { SemanticActionKind } from "./toolCallSemantics";
/** An icon name; the UI owns the icon set, core carries the name as a string. */
type IconName = string;

export type TimelineActivityIconName = IconName;

export type PatchOperation = "add" | "delete" | "update";

export type PatchFileSummary = {
  path: string;
  movePath?: string;
  operation: PatchOperation;
  /** Omitted when the provider did not report an exact line count. */
  added?: number;
  /** Omitted when the provider did not report an exact line count. */
  removed?: number;
};

export interface ActivityTimelineItem {
  type: "activity";
  id: string;
  timestamp?: string;
  statusKey?: string;
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: TimelineActivityIconName;
  activityKind?: "reasoning" | "update";
  streaming?: boolean;
  detail?: string;
  body?: string;
  bodyKind?: "terminal" | "diff-stat";
  /** Primary command shown in expanded details (not the collapsed title). */
  commandText?: string;
  /** Search / lookup query for expanded details. */
  queryText?: string;
  /** Status summary such as Completed · 7.8s or Exit 1. */
  statusLine?: string;
  files?: string[];
  fileSummaries?: PatchFileSummary[];
  previewPath?: string;
  defaultExpanded?: boolean;
  accessibilityLabel?: string;
  providerToolId?: string;
  developerDetails?: ToolDeveloperDetails;
  children?: ActivityChild[];
  /**
   * What the call did, when the provider payload says. Tool rows that carry it
   * may fold into a run summary; rows without it always stay on their own.
   */
  semanticKind?: SemanticActionKind;
  /** A folded run: the finished rows it stands for, in order. */
  steps?: ActivityTimelineItem[];
}

export type ActivityChild = {
  id: string;
  title: string;
  tone?: "neutral" | "running" | "success" | "failed";
  providerToolId?: string;
};
