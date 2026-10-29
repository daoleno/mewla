import type { WorkerApproval, WorkerPrompt } from "../../store/workers";

export type TerminalActionPromptOption = {
  id: string;
  label: string;
  description?: string;
  /** The menu number answered through the daemon's prompt answer; unset when the agent takes the answer itself. */
  number?: number;
  /** Set when the agent reported the decision: the answer goes to it, not to the prompt. */
  approvalOptionId?: string;
  primary?: boolean;
  destructive?: boolean;
  default?: boolean;
};

export type TerminalActionPrompt = {
  id: string;
  title: string;
  requestLabel?: string;
  requestText?: string;
  defaultOptionId?: string;
  options: TerminalActionPromptOption[];
  /** The agent's own id for a decision it reported; its options answer through it. */
  approvalId?: string;
  /** The daemon's id of the terminal prompt behind this card; its options answer by number. */
  promptId?: string;
};

const NEGATIVE_OPTION_RE = /^(?:no|deny|reject|cancel)\b/i;

/**
 * A decision the agent reported itself. It states exactly what is asked and
 * what can be answered, so nothing is read off the screen.
 */
export function buildApprovalPrompt(approval: WorkerApproval, scopeKey?: string): TerminalActionPrompt {
  const options: TerminalActionPromptOption[] = approval.options.map((option) => ({
    id: option.id,
    label: option.label,
    approvalOptionId: option.id,
    destructive: option.scope === "deny",
  }));
  const allow = approval.options.find((option) => option.scope === "once") ?? approval.options.find((option) => option.scope !== "deny");
  const primary = options.find((option) => option.id === allow?.id);
  if (primary) {
    primary.primary = true;
    primary.default = true;
  }
  return scopePrompt({
    id: `approval:${approval.id}`,
    title: "Waiting for confirmation",
    requestLabel: approval.tool || undefined,
    requestText: approval.summary ? redactSensitiveText(approval.summary) : undefined,
    defaultOptionId: primary?.id,
    options,
    approvalId: approval.id,
  }, scopeKey);
}

/**
 * A permission, plan, startup or approval menu the daemon read from the
 * Session's terminal. Its options answer by number through the daemon, which
 * checks the prompt is still the one open. A choice prompt is not a menu: it
 * has its own card.
 */
export function buildMenuPrompt(prompt: WorkerPrompt, scopeKey?: string): TerminalActionPrompt | null {
  const menu = prompt.kind === "choice" ? undefined : prompt.options;
  if (!menu || menu.length === 0) {
    return null;
  }
  const options: TerminalActionPromptOption[] = menu.map((option) => ({
    id: String(option.number),
    label: option.label,
    description: option.description,
    number: option.number,
    destructive: NEGATIVE_OPTION_RE.test(option.label.trim()),
    default: option.selected === true,
    primary: option.selected === true,
  }));
  return scopePrompt({
    id: prompt.id,
    title: prompt.title || "Waiting for confirmation",
    requestText: prompt.request ? redactSensitiveLines(prompt.request) : undefined,
    defaultOptionId: options.find((option) => option.default)?.id,
    options,
    promptId: prompt.id,
  }, scopeKey);
}

function isQuestionApproval(approval: WorkerApproval) {
  return approval.tool?.trim().toLowerCase() === "askuserquestion";
}

/**
 * The card above the composer, or null. A card exists only when the app can
 * answer it: a decision the agent reported, or a menu prompt the daemon read
 * from the terminal. A Session that is blocked without either shows no card.
 */
export function buildTerminalActionPrompt({
  prompt,
  approvals,
  scopeKey,
}: {
  /** Decisions the agent reported; the first one is the card. */
  approvals?: WorkerApproval[];
  /** The blocking prompt the daemon read from the terminal. */
  prompt?: WorkerPrompt;
  /** Live process/session identity so a switched generation cannot reuse card state. */
  scopeKey?: string;
}): TerminalActionPrompt | null {
  // AskUserQuestion is a question, not a permission: the generic Allow/Deny
  // card would answer a question it does not show.
  const decisions = approvals?.filter((approval) => !isQuestionApproval(approval));
  if (decisions && decisions.length > 0) {
    return buildApprovalPrompt(decisions[0], scopeKey);
  }
  return prompt ? buildMenuPrompt(prompt, scopeKey) : null;
}

function scopePrompt(prompt: TerminalActionPrompt, scopeKey?: string): TerminalActionPrompt {
  const scope = scopeKey?.trim();
  return scope ? { ...prompt, id: `${scope}|${prompt.id}` } : prompt;
}

/** Redacts each line on its own so a plan or command keeps its line breaks. */
function redactSensitiveLines(value: string) {
  return value
    .split(/\r?\n/)
    .map((line) => redactSensitiveText(line))
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

export function redactSensitiveText(value: string) {
  return value
    .replace(/\b(Bearer\s+)[A-Za-z0-9._~+/=-]+/gi, "$1[redacted]")
    .replace(/\b(Basic\s+)[A-Za-z0-9+/=-]+/gi, "$1[redacted]")
    .replace(
      /\b([A-Z][A-Z0-9_]*(?:TOKEN|KEY|SECRET|PASSWORD|PASS|AUTH|CREDENTIAL)[A-Z0-9_]*)=("[^"]*"|'[^']*'|[^\s]+)/gi,
      "$1=[redacted]",
    )
    .replace(
      /(--?(?:api[-_]?key|token|password|passwd|secret|credential|auth(?:orization)?)(?:=|\s+))("[^"]*"|'[^']*'|[^\s]+)/gi,
      "$1[redacted]",
    )
    .replace(
      /([?&](?:token|api_key|apikey|key|password|secret|access_token|refresh_token|auth|signature)=)[^&\s]+/gi,
      "$1[redacted]",
    )
    .replace(/(https?:\/\/[^:\s/@]+:)[^@\s/]+@/gi, "$1[redacted]@")
    .replace(/\s+/g, " ")
    .trim();
}
