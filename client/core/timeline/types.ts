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

// components/terminal/InterfaceTimelineItemView.tsx
export type TimelineItem =
  | (MessageTimelineItem & { role: "user" })
  | (MessageTimelineItem & { role: "assistant" })
  | ActivityTimelineItem
  | PlanTimelineItem
  | BrainWorkEventTimelineItem
  | TaskNotificationTimelineItem
  | ComputerMessageTimelineItem;

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

// store/brain.tsx
export type BrainWorkStatus =
  | "open"
  | "running"
  | "waiting"
  | "needs_input"
  | "done"
  | "cancelled";

export type BrainWorkWake = {
  kind: "session_terminal" | "calendar_result" | "user_input";
  ref: string;
};

export type BrainWorkProgressMode = "owned" | "waiting" | "ready";

export type BrainSessionFinalization = {
  session_id: string;
  delegated: boolean;
  state: "pending" | "failed" | "complete" | "skipped";
  attempts?: number;
  last_error?: string;
  updated_at: string;
};

export type BrainWorkAttentionState = "queued" | "reserved" | "reviewing";

export type BrainCurrentWork = {
  work_id: string;
  revision: number;
  title: string;
  status: BrainWorkStatus;
  progress_mode?: BrainWorkProgressMode;
  attempt_session_id?: string;
  attempt_delegated?: boolean;
  wait_for?: string;
  wake?: BrainWorkWake;
  attention_state?: BrainWorkAttentionState;
  session_finalizations?: BrainSessionFinalization[];
  unread_result: boolean;
  /** One line about the newest outcome; daemons before 0.2.1 omit it. */
  summary?: string;
  updated_at?: string;
  /** What Brain asks you about this Work, with one-tap answers. */
  question?: string;
  choices?: string[];
  /** When the Work started waiting on a decision; never moves while it waits. */
  attention_since?: string;
  /** The open review's reason, e.g. "turn_lost" for an unknown outcome. */
  attention_reason?: string;
  /** Hidden from Needs you until then; the app compares the time. */
  snoozed_until?: string;
  /** Your newest decision on it, until a newer result comes back. */
  user_action?: BrainWorkUserAction;
  /** The schedule and run a Calendar occurrence's Work came from. */
  calendar?: BrainWorkCalendarRef;
};

export type BrainWorkCalendarRef = { item_id: string; run_id: string };

export type BrainWorkUserAction = {
  kind: "reply" | "close" | "snooze";
  text?: string;
  at: string;
  /** For a reply: Brain's admission ("accepted", "uncertain"). */
  admission?: string;
};

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
