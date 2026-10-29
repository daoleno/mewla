import type {
  CodexConversationEvent,
  ConversationChoice,
  ConversationChoiceQuestion,
} from "../../services/codexConversation";
import type { WorkerPrompt } from "../../store/workers";

/** The pending choice the Interface composer renders as a card. */
export type InterfaceChoicePrompt = {
  /** The same question set keeps the draft; a new one starts over. */
  key: string;
  choice: ConversationChoice;
  /** The daemon connection is up, which is all an answer needs: the daemon re-checks the screen. */
  connected: boolean;
  onSubmit(answers: ChoiceAnswer[]): Promise<void>;
};

/** One question's in-progress answer on the Interface card. */
export type ChoiceDraftAnswer = {
  selected: number[];
  other: string;
  otherActive: boolean;
};

/** One question's answer: zero-based option indexes plus the free text of Other, "" when unused. */
export type ChoiceAnswer = { selected: number[]; other: string };

const MAX_OTHER_LENGTH = 500;

/**
 * The newest choice event, when it is still pending. An older unmatched
 * choice was abandoned (interrupt, restart) and is never answerable.
 */
export function latestPendingChoice(
  events: readonly CodexConversationEvent[] | undefined,
): (CodexConversationEvent & { choice: ConversationChoice }) | null {
  if (!events) {
    return null;
  }
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (!event.choice) {
      continue;
    }
    return event.choice.state === "pending" && event.call_id
      ? (event as CodexConversationEvent & { choice: ConversationChoice })
      : null;
  }
  return null;
}

/** The daemon code for "the question already passed" (answered, dismissed or replaced). */
export const CHOICE_GONE_CODE = "answer_choice_gone";

/** What the person is told when the card disappears because the question passed. */
export const CHOICE_GONE_NOTICE = "That question has already passed.";

/** The daemon code for "the prompt on screen is no longer the one this answer was for". */
export const PROMPT_GONE_CODE = "answer_prompt_gone";

/** What the person is told when a prompt card disappears because the prompt passed. */
export const PROMPT_GONE_NOTICE = "This prompt has already passed.";

/** Whether an answer error means the prompt is no longer open. */
export function isPromptGoneError(reason: unknown): boolean {
  return (
    typeof reason === "object" &&
    reason !== null &&
    (reason as { code?: unknown }).code === PROMPT_GONE_CODE
  );
}

/**
 * The choice card's questions for a choice prompt the daemon read from the
 * terminal, or null when it cannot be answered. The terminal shows one tab at
 * a time, so a question without options is filled from the transcript's
 * pending choice of the same tool call; if any still has none the app cannot
 * drive it, and there is no card.
 */
export function choiceFromPrompt(
  prompt: WorkerPrompt | undefined,
  transcript?: { callId: string; choice: ConversationChoice } | null,
): ConversationChoice | null {
  if (!prompt || prompt.kind !== "choice" || !prompt.questions?.length) {
    return null;
  }
  const fill =
    transcript && prompt.call_id && transcript.callId === prompt.call_id
      ? transcript.choice
      : null;
  const questions: ConversationChoiceQuestion[] = [];
  for (let index = 0; index < prompt.questions.length; index += 1) {
    const source = prompt.questions[index];
    if (source.options.length > 0) {
      questions.push({
        question: source.question,
        header: source.header,
        multi_select: source.multi_select,
        allows_other: source.allows_other === true,
        options: source.options.map((option) => ({
          label: option.label,
          description: option.description,
        })),
      });
      continue;
    }
    const filled = fill?.questions.length === prompt.questions.length ? fill.questions[index] : undefined;
    if (!filled || filled.options.length === 0) {
      return null;
    }
    questions.push({ ...filled, header: source.header ?? filled.header });
  }
  return { kind: "AskUserQuestion", state: "pending", questions };
}

/**
 * The pending choice the card should show: the latest pending one, unless the
 * daemon already said its question passed (the transcript can lag the screen).
 */
export function visiblePendingChoice(
  events: readonly CodexConversationEvent[] | undefined,
  gone: ReadonlySet<string>,
): (CodexConversationEvent & { choice: ConversationChoice }) | null {
  const pending = latestPendingChoice(events);
  return pending?.call_id && gone.has(pending.call_id) ? null : pending;
}

/**
 * Whether the agent is waiting on a question: its newest choice has no result
 * yet. Unlike the card, this ignores the daemon's "passed", so the Working row
 * stays away until the transcript settles the question.
 */
export function awaitingChoiceAnswer(
  events: readonly CodexConversationEvent[] | undefined,
): boolean {
  return latestPendingChoice(events) !== null;
}

/** Whether an answer error means the question is no longer on screen. */
export function isChoiceGoneError(reason: unknown): boolean {
  return (
    typeof reason === "object" &&
    reason !== null &&
    (reason as { code?: unknown }).code === CHOICE_GONE_CODE
  );
}

export function questionAllowsOther(question: ConversationChoiceQuestion) {
  return question.allows_other ?? !question.options.some((option) => Boolean(option.preview));
}

export function emptyChoiceDraft(choice: ConversationChoice): ChoiceDraftAnswer[] {
  return choice.questions.map(() => ({ selected: [], other: "", otherActive: false }));
}

export function toggleChoiceOption(
  choice: ConversationChoice,
  draft: ChoiceDraftAnswer[],
  questionIndex: number,
  optionIndex: number,
): ChoiceDraftAnswer[] {
  const question = choice.questions[questionIndex];
  if (!question || optionIndex < 0 || optionIndex >= question.options.length) {
    return draft;
  }
  return draft.map((answer, index) => {
    if (index !== questionIndex) {
      return answer;
    }
    if (!question.multi_select) {
      return { ...answer, selected: [optionIndex], otherActive: false };
    }
    const selected = answer.selected.includes(optionIndex)
      ? answer.selected.filter((value) => value !== optionIndex)
      : [...answer.selected, optionIndex].sort((a, b) => a - b);
    return { ...answer, selected };
  });
}

export function toggleChoiceOther(
  choice: ConversationChoice,
  draft: ChoiceDraftAnswer[],
  questionIndex: number,
): ChoiceDraftAnswer[] {
  const question = choice.questions[questionIndex];
  if (!question || !questionAllowsOther(question)) {
    return draft;
  }
  return draft.map((answer, index) => {
    if (index !== questionIndex) {
      return answer;
    }
    if (!question.multi_select) {
      return { ...answer, selected: [], otherActive: true };
    }
    return { ...answer, otherActive: !answer.otherActive };
  });
}

export function setChoiceOtherText(
  draft: ChoiceDraftAnswer[],
  questionIndex: number,
  text: string,
): ChoiceDraftAnswer[] {
  // Claude's free-text row is a single line; the daemon rejects control characters.
  const other = text.replace(/[\r\n\t]+/g, " ").slice(0, MAX_OTHER_LENGTH);
  return draft.map((answer, index) =>
    index === questionIndex ? { ...answer, other } : answer,
  );
}

function answerOther(answer: ChoiceDraftAnswer) {
  return answer.otherActive ? answer.other.trim() : "";
}

export function choiceQuestionAnswered(
  question: ConversationChoiceQuestion,
  answer: ChoiceDraftAnswer | undefined,
) {
  if (!answer) {
    return false;
  }
  if (answer.otherActive && !answerOther(answer)) {
    return false;
  }
  const count = answer.selected.length + (answerOther(answer) ? 1 : 0);
  return question.multi_select ? count > 0 : count === 1;
}

export function choiceDraftComplete(choice: ConversationChoice, draft: ChoiceDraftAnswer[]) {
  return choice.questions.every((question, index) =>
    choiceQuestionAnswered(question, draft[index]),
  );
}

export function buildChoiceAnswers(
  choice: ConversationChoice,
  draft: ChoiceDraftAnswer[],
): ChoiceAnswer[] | null {
  if (!choiceDraftComplete(choice, draft)) {
    return null;
  }
  return choice.questions.map((_, index) => {
    const answer = draft[index];
    return { selected: answer.selected, other: answerOther(answer) };
  });
}

/** Room the composer needs under the card, in dp. */
const CHOICE_CARD_COMPOSER_RESERVE = 112;
const CHOICE_CARD_MIN_HEIGHT = 200;

/**
 * The tallest the card may be so it never reaches the floating header: the
 * card sits in the composer overlay, which has no height limit of its own.
 */
export function choiceCardMaxHeight(input: {
  windowHeight: number;
  topChromeInset: number;
  bottomInset: number;
}) {
  const room =
    input.windowHeight - input.topChromeInset - input.bottomInset - CHOICE_CARD_COMPOSER_RESERVE - 8;
  return Math.max(CHOICE_CARD_MIN_HEIGHT, Math.floor(room));
}

/**
 * The wizard's position: 0..n-1 is a question, n is the review step. Like the
 * Claude Code terminal, one question shows at a time and the last step lists
 * every answer before it is sent.
 */
export function choiceReviewStep(choice: ConversationChoice) {
  return choice.questions.length;
}

export function clampChoiceStep(choice: ConversationChoice, step: number) {
  return Math.min(Math.max(Math.trunc(step) || 0, 0), choiceReviewStep(choice));
}

/** The step a single-select pick moves to: the next question, else review. */
export function nextChoiceStep(choice: ConversationChoice, step: number) {
  return clampChoiceStep(choice, step + 1);
}

export function unansweredChoiceQuestions(choice: ConversationChoice, draft: ChoiceDraftAnswer[]) {
  const missing: number[] = [];
  choice.questions.forEach((question, index) => {
    if (!choiceQuestionAnswered(question, draft[index])) {
      missing.push(index);
    }
  });
  return missing;
}

/** "Cloud SQL" or "US East, Asia, Oceania": the labels and Other text chosen. */
export function choiceDraftAnswerText(
  question: ConversationChoiceQuestion,
  answer: ChoiceDraftAnswer | undefined,
) {
  if (!answer || !choiceQuestionAnswered(question, answer)) {
    return "";
  }
  const labels = answer.selected
    .map((index) => question.options[index]?.label ?? "")
    .filter(Boolean);
  const other = answerOther(answer);
  return (other ? [...labels, other] : labels).join(", ");
}

/** "Fruit: Banana · Colors: Red, Blue" for the resolved timeline line. */
export function choiceAnsweredSummary(choice: ConversationChoice): string {
  return choiceAnswerLines(choice).join(" · ");
}

/** One "Question → answer" line per question for the expanded row. */
export function choiceAnswerDetails(choice: ConversationChoice): string {
  const answers = choice.answers ?? [];
  return choice.questions
    .map((question, index) => `${question.question.trim()}\n→ ${answers[index]?.trim() || "No answer"}`)
    .join("\n\n");
}

function choiceAnswerLines(choice: ConversationChoice): string[] {
  if (choice.state === "declined") {
    return ["Dismissed"];
  }
  if (choice.state === "unanswered") {
    return ["Not answered"];
  }
  const answers = choice.answers ?? [];
  return choice.questions
    .map((question, index) => {
      const answer = answers[index]?.trim() || "No answer";
      const label = question.header?.trim() || question.question.trim();
      return choice.questions.length > 1 || question.header ? `${label}: ${answer}` : answer;
    });
}
