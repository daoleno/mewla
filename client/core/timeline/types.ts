/**
 * The timeline's item types, which the Expo app declared beside its views and
 * stores. Copied as type text; core has no views.
 */
import type { CodexPlanStep } from "../conversation/conversation";
import type { PendingUserMessageLifecycle } from "../conversation/pendingUserMessages";
import type { ActivityTimelineItem } from "./activityTypes";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type { ComputerMessageTimelineItem } from "./computerMessageCardModel";
import type { TaskNotificationTimelineItem } from "./taskNotificationCardModel";
import type { BrainCurrentWork } from "../work/types";
import type { AnsweredRequest } from "./answered";

// components/terminal/InterfaceTimelineItemView.tsx
export type TimelineItem =
  | (MessageTimelineItem & { role: "user" })
  | (MessageTimelineItem & { role: "assistant" })
  | ActivityTimelineItem
  | PlanTimelineItem
  | BrainWorkEventTimelineItem
  | TaskNotificationTimelineItem
  | ComputerMessageTimelineItem
  | AnsweredTimelineItem;

/** An answered request's sealed line, where it was asked (WIRE §2); `keys` are its card keys. */
export type AnsweredTimelineItem = {
  type: "answered";
  id: string;
  timestamp?: string;
  record: AnsweredRequest;
  keys: readonly string[];
};

// components/terminal/InterfaceTimelineMessage.tsx
export type DisplayAttachment = {
  name: string;
  path: string;
  localUri?: string;
  mimeType?: string;
};

export interface MessageTimelineItem {
  type: "message";
  id: string;
  role: "user" | "assistant";
  timestamp?: string;
  body: string;
  attachments: DisplayAttachment[];
  pending?: boolean;
  pendingLifecycle?: PendingUserMessageLifecycle;
  pendingAccessibilityLabel?: string;
  onRetryPending?: () => void;
  onDiscardPending?: () => void;
  streaming?: boolean;

  /** Process-local presentation alias; provider id/body remain canonical. */
  turnFocusAnchorId?: string;
}

// components/terminal/InterfaceTimelinePlanTypes.ts
export interface PlanTimelineItem {
  type: "plan";
  id: string;
  timestamp?: string;
  explanation?: string;
  steps: CodexPlanStep[];
}

// components/terminal/InterfaceTimelineDateDivider.tsx
export type DateDividerItem = {
  type: "date-divider";
  id: string;
  label: string;
};

// components/brain/BrainWorkEventCard.tsx
export type BrainWorkEventTimelineItem = {
  type: "brain-work-event";
  id: string;
  timestamp: string;
  event: BrainWorkResultEvent;
  events: BrainWorkResultEvent[];
  sourceCount?: number;
  currentWork?: BrainCurrentWork;
  onPress?: () => void;
  /** Brain only: the cat sits on this slip because its Work needs you. */
  catPerched?: boolean;
};

// Brain's Work: one source (core/work/types).
export type { BrainCurrentWork, BrainSessionFinalization, BrainWorkAttentionState, BrainWorkCalendarRef, BrainWorkProgressMode, BrainWorkStatus, BrainWorkUserAction, BrainWorkWake } from "../work/types";

// store/workers.tsx
export type WorkerPromptKind = 'permission' | 'plan' | 'choice' | 'startup' | 'approval';

/** One row of a prompt's numbered menu; `number` is the digit that picks it. */
export interface WorkerPromptOption {
  number: number;
  label: string;
  description?: string;
  selected?: boolean;
}

/** One question (tab) of a choice prompt. */
export interface WorkerPromptQuestion {
  header?: string;
  question: string;
  multi_select?: boolean;
  allows_other?: boolean;
  answered?: boolean;
  options: WorkerPromptOption[];
}

/**
 * A blocking prompt the daemon read from the Session's terminal. Menu kinds
 * (permission, plan, startup, approval) carry `options` and are answered by
 * option number; a `choice` prompt carries `questions`.
 */
export interface WorkerPrompt {
  id: string;
  kind: WorkerPromptKind;
  provider: string;
  title: string;
  request?: string;
  options?: WorkerPromptOption[];
  questions?: WorkerPromptQuestion[];
  call_id?: string;
}

// services/sessionFilePreview.ts
export interface SessionFileBinarySource {
  uri: string;
  headers: Record<string, string>;
  mimeType?: string;
}
