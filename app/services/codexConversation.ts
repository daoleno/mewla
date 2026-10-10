export type CodexConversationEventKind =
  | "user_message"
  | "assistant_message"
  | "commentary"
  | "command"
  | "tool"
  | "web_search"
  | "patch"
  | "plan"
  | "status";

export type CodexConversationRole = "user" | "assistant";
export type CodexPlanStepStatus = "pending" | "in_progress" | "completed";

export type ProviderActivityStatus =
  "running" | "completed" | "failed" | "interrupted" | "cancelled";
export type WorkReviewState =
  | "queued"
  | "reserved"
  | "reviewing"
  | "resolved";
export type WorkSessionState =
  | "open"
  | "closing"
  | "finalized"
  | "close_failed"
  | "not_required";

/**
 * Provider-neutral executor lifecycle. Transcript events are deliberately not
 * part of this record: partial text and tool rendering cannot open or settle
 * Activity.
 */
export interface ProviderActivity {
  id: string;
  status: ProviderActivityStatus;
  started_at: string;
  settled_at?: string;
}

export interface CodexPlanStep {
  step: string;
  status: CodexPlanStepStatus;
}

export type CodexConversationFileOperation = "add" | "delete" | "update";

export interface CodexConversationFileChange {
  path: string;
  move_path?: string;
  operation: CodexConversationFileOperation;
  additions?: number;
  deletions?: number;
}

/** "unanswered": the conversation moved on without a result (interrupt, restart). */
export type ConversationChoiceState = "pending" | "answered" | "declined" | "unanswered";

export interface ConversationChoiceOption {
  label: string;
  description?: string;
  preview?: string;
}

export interface ConversationChoiceQuestion {
  question: string;
  header?: string;
  multi_select?: boolean;
  options: ConversationChoiceOption[];
}

/** A provider choice prompt (Claude AskUserQuestion) read from the transcript. */
export interface ConversationChoice {
  kind: string;
  state: ConversationChoiceState;
  questions: ConversationChoiceQuestion[];
  /** Recorded answers, aligned with questions, once answered. */
  answers?: string[];
}

export interface CodexConversationEvent {
  id: string;
  seq: number;
  timestamp?: string;
  kind: CodexConversationEventKind;
  role?: CodexConversationRole;
  title?: string;
  body?: string;
  command?: string;
  tool_name?: string;
  input?: string;
  output?: string;
  call_id?: string;
  exit_code?: number;
  status?: string;
  partial?: boolean;
  transient?: boolean;
  files?: string[];
  file_changes?: CodexConversationFileChange[];
  explanation?: string;
  plan?: CodexPlanStep[];
  choice?: ConversationChoice;
  source?: string;
  work_id?: string;
  work_session_id?: string;
  session_name?: string;
  unread?: boolean;
  work_review_state?: WorkReviewState;
  work_session_state?: WorkSessionState;
  work_result_current?: boolean;
  work_phase?: string;
  work_attention?: string;
  work_event_kind?: string;
  work_details_json?: string;
  work_next_action?: string;
  work_wait_for?: string;
  /** Task fields are set only for source "task_notification" status events. */
  task_id?: string;
  task_tokens?: number;
  task_tool_uses?: number;
  task_duration_ms?: number;
}

export interface CodexConversation {
  available: boolean;
  reason?: string;
  source?: string;
  path?: string;
  session_id?: string;
  cwd?: string;
  updated_at?: string;
  /** The provider's sole lifecycle fact for Working, timer, and Stop. */
  activity?: ProviderActivity;
  events: CodexConversationEvent[];
}

export function normalizeCodexConversation(value: any): CodexConversation {
  const conversation = value && typeof value === "object" ? value : {};
  return {
    available: Boolean(conversation.available),
    reason:
      typeof conversation.reason === "string" ? conversation.reason : undefined,
    source:
      typeof conversation.source === "string" ? conversation.source : undefined,
    path: typeof conversation.path === "string" ? conversation.path : undefined,
    session_id:
      typeof conversation.session_id === "string"
        ? conversation.session_id
        : undefined,
    cwd: typeof conversation.cwd === "string" ? conversation.cwd : undefined,
    updated_at:
      typeof conversation.updated_at === "string"
        ? conversation.updated_at
        : undefined,
    activity: normalizeProviderActivity(conversation.activity),
    events: Array.isArray(conversation.events)
      ? conversation.events.map(normalizeCodexConversationEvent).filter(Boolean)
      : [],
  };
}

export function normalizeProviderActivity(
  value: unknown,
): ProviderActivity | undefined {
  const activity =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : null;
  if (
    !activity ||
    typeof activity.id !== "string" ||
    !activity.id.trim() ||
    typeof activity.started_at !== "string" ||
    !activity.started_at ||
    !Number.isFinite(Date.parse(activity.started_at))
  ) {
    return undefined;
  }
  const status = normalizeProviderActivityStatus(activity.status);
  if (!status) {
    return undefined;
  }
  return {
    id: activity.id.trim(),
    status,
    started_at: activity.started_at,
    settled_at:
      typeof activity.settled_at === "string" &&
      activity.settled_at &&
      Number.isFinite(Date.parse(activity.settled_at))
        ? activity.settled_at
        : undefined,
  };
}

export function isProviderActivityRunning(
  activity?: ProviderActivity | null,
): activity is ProviderActivity & { status: "running" } {
  return activity?.status === "running";
}

export function isProviderActivityTerminal(
  activity?: ProviderActivity | null,
): boolean {
  return Boolean(
    activity &&
    (activity.status === "completed" ||
      activity.status === "failed" ||
      activity.status === "interrupted" ||
      activity.status === "cancelled"),
  );
}

function normalizeProviderActivityStatus(
  value: unknown,
): ProviderActivityStatus | undefined {
  switch (value) {
    case "running":
    case "completed":
    case "failed":
    case "interrupted":
    case "cancelled":
      return value;
    default:
      return undefined;
  }
}

function normalizeCodexConversationEvent(
  value: any,
): CodexConversationEvent | null {
  const event = value && typeof value === "object" ? value : {};
  if (isGoalInternalContextEvent(event)) {
    return null;
  }
  const kind = normalizeKind(event.kind);
  if (!kind) {
    return null;
  }
  const id =
    typeof event.id === "string" && event.id
      ? event.id
      : `${kind}:${event.seq ?? ""}`;
  const normalized: CodexConversationEvent = {
    id,
    seq:
      typeof event.seq === "number" && Number.isFinite(event.seq)
        ? event.seq
        : 0,
    timestamp:
      typeof event.timestamp === "string" ? event.timestamp : undefined,
    kind,
    role:
      event.role === "user" || event.role === "assistant"
        ? event.role
        : undefined,
    title: sanitizeGoalInternalContext(event.title),
    body: sanitizeGoalInternalContext(event.body),
    command: sanitizeGoalInternalContext(event.command),
    tool_name:
      typeof event.tool_name === "string" ? event.tool_name : undefined,
    input: sanitizeGoalInternalContext(event.input),
    output: sanitizeGoalInternalContext(event.output),
    call_id: typeof event.call_id === "string" ? event.call_id : undefined,
    exit_code:
      typeof event.exit_code === "number" && Number.isFinite(event.exit_code)
        ? event.exit_code
        : undefined,
    status: typeof event.status === "string" ? event.status : undefined,
    partial: typeof event.partial === "boolean" ? event.partial : undefined,
    transient:
      typeof event.transient === "boolean" ? event.transient : undefined,
    files: Array.isArray(event.files)
      ? event.files.filter(
          (file: unknown): file is string => typeof file === "string",
        )
      : undefined,
    file_changes: Array.isArray(event.file_changes)
      ? event.file_changes
          .map(normalizeFileChange)
          .filter(
            (
              change: CodexConversationFileChange | null,
            ): change is CodexConversationFileChange => Boolean(change),
          )
      : undefined,
    explanation: sanitizeGoalInternalContext(event.explanation),
    plan: Array.isArray(event.plan)
      ? event.plan
          .map(normalizePlanStep)
          .filter((step: CodexPlanStep | null): step is CodexPlanStep =>
            Boolean(step),
          )
      : undefined,
    choice: normalizeConversationChoice(event.choice),
    source: typeof event.source === "string" ? event.source : undefined,
    work_id: typeof event.work_id === "string" ? event.work_id : undefined,
    work_session_id:
      typeof event.work_session_id === "string"
        ? event.work_session_id
        : undefined,
    session_name:
      typeof event.session_name === "string" ? event.session_name : undefined,
    unread: typeof event.unread === "boolean" ? event.unread : undefined,
    work_review_state: normalizeWorkReviewState(event.work_review_state),
    work_session_state: normalizeWorkSessionState(event.work_session_state),
    work_result_current:
      typeof event.work_result_current === "boolean"
        ? event.work_result_current
        : undefined,
    work_phase:
      typeof event.work_phase === "string" ? event.work_phase : undefined,
    work_attention:
      typeof event.work_attention === "string"
        ? event.work_attention
        : undefined,
    work_event_kind:
      typeof event.work_event_kind === "string"
        ? event.work_event_kind
        : undefined,
    work_details_json:
      typeof event.work_details_json === "string"
        ? event.work_details_json
        : undefined,
    work_next_action:
      typeof event.work_next_action === "string"
        ? event.work_next_action
        : undefined,
    work_wait_for:
      typeof event.work_wait_for === "string"
        ? event.work_wait_for
        : undefined,
    task_id: typeof event.task_id === "string" ? event.task_id : undefined,
    task_tokens: finiteCount(event.task_tokens),
    task_tool_uses: finiteCount(event.task_tool_uses),
    task_duration_ms: finiteCount(event.task_duration_ms),
  };
  if (
    (normalized.kind === "user_message" ||
      normalized.kind === "assistant_message") &&
    !normalized.body
  ) {
    return null;
  }
  return normalized;
}

function finiteCount(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : undefined;
}

function normalizeWorkReviewState(value: unknown): WorkReviewState | undefined {
  return value === "queued" ||
    value === "reserved" ||
    value === "reviewing" ||
    value === "resolved"
    ? value
    : undefined;
}

function normalizeWorkSessionState(
  value: unknown,
): WorkSessionState | undefined {
  switch (value) {
    case "open":
    case "closing":
    case "finalized":
    case "close_failed":
    case "not_required":
      return value;
    default:
      return undefined;
  }
}

const CODEX_GOAL_INTERNAL_CONTEXT_RE =
  /<codex_internal_context\b[^>]*\bsource\s*=\s*["']goal["'][^>]*>.*?<\/codex_internal_context\s*>|<codex_internal_context\b[^>]*\bsource\s*=\s*["']goal["'][^>]*\/\s*>/gis;

function sanitizeGoalInternalContext(value: unknown): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const sanitized = value
    .replace(CODEX_GOAL_INTERNAL_CONTEXT_RE, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
  return sanitized || undefined;
}

function isGoalInternalContextEvent(event: Record<string, unknown>) {
  const source =
    typeof event.source === "string" ? event.source.trim().toLowerCase() : "";
  const title =
    typeof event.title === "string" ? event.title.trim().toLowerCase() : "";
  const toolName =
    typeof event.tool_name === "string"
      ? event.tool_name.trim().toLowerCase()
      : "";
  return (
    source === "goal" ||
    source === "codex_internal_context" ||
    source === "codex_internal_context:goal" ||
    title === "codex_internal_context" ||
    toolName === "codex_internal_context"
  );
}

function normalizeFileChange(
  value: unknown,
): CodexConversationFileChange | null {
  const change =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : null;
  if (!change || typeof change.path !== "string" || !change.path.trim()) {
    return null;
  }
  const operation = normalizeFileOperation(change.operation);
  if (!operation) {
    return null;
  }
  const additions = normalizeLineCount(change.additions);
  const deletions = normalizeLineCount(change.deletions);
  return {
    path: change.path.trim(),
    move_path:
      typeof change.move_path === "string" && change.move_path.trim()
        ? change.move_path.trim()
        : undefined,
    operation,
    additions,
    deletions,
  };
}

function normalizeFileOperation(
  value: unknown,
): CodexConversationFileOperation | undefined {
  switch (value) {
    case "add":
    case "delete":
    case "update":
      return value;
    default:
      return undefined;
  }
}

function normalizeLineCount(value: unknown): number | undefined {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
    ? value
    : undefined;
}

function normalizePlanStep(value: any): CodexPlanStep | null {
  const step = value && typeof value === "object" ? value : {};
  const text = sanitizeGoalInternalContext(step.step);
  if (!text) {
    return null;
  }
  const status =
    step.status === "completed" ||
    step.status === "in_progress" ||
    step.status === "pending"
      ? step.status
      : "pending";
  return {
    step: text,
    status,
  };
}

function normalizeKind(value: unknown): CodexConversationEventKind | null {
  switch (value) {
    case "user_message":
    case "assistant_message":
    case "commentary":
    case "command":
    case "tool":
    case "web_search":
    case "patch":
    case "plan":
    case "status":
      return value;
    // The OpenCode, Pi and DSH adapters still emit these kinds; they map onto
    // the canonical projection vocabulary at this single wire boundary, so
    // Interface tool/reasoning cards render without a per-provider UI fork.
    case "tool_call":
      return "tool";
    case "reasoning":
      return "commentary";
    default:
      return null;
  }
}

export function conversationChoicesEqual(
  left: ConversationChoice | undefined,
  right: ConversationChoice | undefined,
) {
  return left === right || JSON.stringify(left) === JSON.stringify(right);
}

function normalizeConversationChoice(value: unknown): ConversationChoice | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const raw = value as Record<string, unknown>;
  const state = raw.state;
  if (state !== "pending" && state !== "answered" && state !== "declined" && state !== "unanswered") {
    return undefined;
  }
  if (!Array.isArray(raw.questions) || raw.questions.length === 0) {
    return undefined;
  }
  const questions: ConversationChoiceQuestion[] = [];
  for (const item of raw.questions) {
    const question = item && typeof item === "object" ? (item as Record<string, unknown>) : null;
    if (!question || typeof question.question !== "string" || !Array.isArray(question.options)) {
      return undefined;
    }
    const options: ConversationChoiceOption[] = [];
    for (const optionItem of question.options) {
      const option =
        optionItem && typeof optionItem === "object" ? (optionItem as Record<string, unknown>) : null;
      if (!option || typeof option.label !== "string") {
        return undefined;
      }
      options.push({
        label: option.label,
        description: typeof option.description === "string" ? option.description : undefined,
        preview: typeof option.preview === "string" ? option.preview : undefined,
      });
    }
    questions.push({
      question: question.question,
      header: typeof question.header === "string" ? question.header : undefined,
      multi_select: question.multi_select === true,
      options,
    });
  }
  return {
    kind: typeof raw.kind === "string" ? raw.kind : "",
    state,
    questions,
    answers: Array.isArray(raw.answers)
      ? raw.answers.map((answer) => (typeof answer === "string" ? answer : ""))
      : undefined,
  };
}
