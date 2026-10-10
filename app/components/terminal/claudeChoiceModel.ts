import type {
  CodexConversationEvent,
  ConversationChoice,
  ConversationChoiceQuestion,
} from "../../services/codexConversation";

/** The pending choice the Interface composer renders as a card. */
export type InterfaceChoicePrompt = {
  callId: string;
  choice: ConversationChoice;
  /** The live pane shows the prompt, so an answer can be sent. */
  live: boolean;
  onSubmit(payload: ChoiceAnswerPayload): Promise<void>;
};

/** One question's in-progress answer on the Interface card. */
export type ChoiceDraftAnswer = {
  selected: number[];
  other: string;
  otherActive: boolean;
};

export type ChoiceAnswerPayload = {
  call_id: string;
  answers: Array<{ selected: number[]; other?: string }>;
};

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

export function questionAllowsOther(question: ConversationChoiceQuestion) {
  return !question.options.some((option) => Boolean(option.preview));
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

export function buildChoiceAnswerPayload(
  callId: string,
  choice: ConversationChoice,
  draft: ChoiceDraftAnswer[],
): ChoiceAnswerPayload | null {
  if (!callId || !choiceDraftComplete(choice, draft)) {
    return null;
  }
  return {
    call_id: callId,
    answers: choice.questions.map((_, index) => {
      const answer = draft[index];
      const other = answerOther(answer);
      return other
        ? { selected: answer.selected, other }
        : { selected: answer.selected };
    }),
  };
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
