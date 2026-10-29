import { describe, expect, test } from "bun:test";
import {
  normalizeCodexConversation,
  type CodexConversationEvent,
  type ConversationChoice,
} from "../../services/codexConversation";
import {
  buildChoiceAnswers,
  choiceFromPrompt,
  isPromptGoneError,
  PROMPT_GONE_CODE,
  choiceAnsweredSummary,
  choiceCardMaxHeight,
  choiceDraftAnswerText,
  choiceDraftComplete,
  choiceReviewStep,
  clampChoiceStep,
  nextChoiceStep,
  unansweredChoiceQuestions,
  emptyChoiceDraft,
  latestPendingChoice,
  awaitingChoiceAnswer,
  visiblePendingChoice,
  isChoiceGoneError,
  CHOICE_GONE_CODE,
  setChoiceOtherText,
  toggleChoiceOption,
  toggleChoiceOther,
  questionAllowsOther,
} from "./claudeChoiceModel";
import { buildTimeline } from "./InterfaceTimelineModel";

const choice: ConversationChoice = {
  kind: "AskUserQuestion",
  state: "pending",
  questions: [
    {
      question: "Which fruit?",
      header: "Fruit",
      options: [
        { label: "Apple", description: "Red and crisp" },
        { label: "Banana", description: "Yellow" },
      ],
    },
    {
      question: "Which colors?",
      header: "Colors",
      multi_select: true,
      options: [
        { label: "Red", description: "warm" },
        { label: "Green", description: "calm" },
        { label: "Blue", description: "cool" },
      ],
    },
  ],
};

function choiceEvent(id: string, state: ConversationChoice["state"], answers?: string[]): CodexConversationEvent {
  return {
    id,
    seq: 1,
    kind: "tool",
    tool_name: "AskUserQuestion",
    call_id: id,
    status: state === "pending" ? "running" : "done",
    choice: { ...choice, state, answers },
  };
}

describe("claude choice model", () => {
  test("normalizes the daemon choice payload", () => {
    const conversation = normalizeCodexConversation({
      available: true,
      events: [
        {
          id: "claude-tool:toolu_1",
          seq: 1,
          kind: "tool",
          call_id: "toolu_1",
          choice: {
            kind: "AskUserQuestion",
            state: "answered",
            questions: [
              { question: "Pick?", multi_select: true, options: [{ label: "A", preview: "[]" }] },
            ],
            answers: ["A"],
          },
        },
        { id: "bad", seq: 2, kind: "tool", choice: { state: "weird", questions: [] } },
      ],
    });
    expect(conversation.events[0].choice).toEqual({
      kind: "AskUserQuestion",
      state: "answered",
      questions: [
        {
          question: "Pick?",
          header: undefined,
          multi_select: true,
          options: [{ label: "A", description: undefined, preview: "[]" }],
        },
      ],
      answers: ["A"],
    });
    expect(conversation.events[1].choice).toBeUndefined();
  });

  test("only the newest choice can be pending", () => {
    expect(latestPendingChoice([choiceEvent("a", "pending")])?.call_id).toBe("a");
    expect(latestPendingChoice([choiceEvent("a", "pending"), choiceEvent("b", "answered", ["Apple", "Red"])])).toBeNull();
    expect(
      latestPendingChoice([choiceEvent("a", "answered"), { id: "m", seq: 2, kind: "assistant_message", body: "hi" }, choiceEvent("b", "pending")])
        ?.call_id,
    ).toBe("b");
  });

  test("builds the exact answer payload", () => {
    let draft = emptyChoiceDraft(choice);
    expect(choiceDraftComplete(choice, draft)).toBe(false);
    draft = toggleChoiceOption(choice, draft, 0, 0);
    draft = toggleChoiceOption(choice, draft, 0, 1);
    draft = toggleChoiceOption(choice, draft, 1, 2);
    draft = toggleChoiceOption(choice, draft, 1, 0);
    draft = toggleChoiceOther(choice, draft, 1);
    expect(buildChoiceAnswers(choice, draft)).toBeNull();
    draft = setChoiceOtherText(draft, 1, " Mauve\n紫 ");
    expect(buildChoiceAnswers(choice, draft)).toEqual([
      { selected: [1], other: "" },
      { selected: [0, 2], other: "Mauve 紫" },
    ]);
  });

  test("single-select Other replaces the option", () => {
    let draft = toggleChoiceOption(choice, emptyChoiceDraft(choice), 0, 0);
    draft = toggleChoiceOther(choice, draft, 0);
    draft = setChoiceOtherText(draft, 0, "Cherry");
    draft = toggleChoiceOption(choice, draft, 1, 1);
    expect(buildChoiceAnswers(choice, draft)?.[0]).toEqual({
      selected: [],
      other: "Cherry",
    });
  });

  test("preview questions have no Other", () => {
    const preview: ConversationChoice = {
      ...choice,
      questions: [{ question: "Layout?", options: [{ label: "Grid", preview: "[][]" }, { label: "List" }] }],
    };
    const draft = toggleChoiceOther(preview, emptyChoiceDraft(preview), 0);
    expect(draft[0].otherActive).toBe(false);
  });

  test("summarizes answered and dismissed choices", () => {
    expect(choiceAnsweredSummary({ ...choice, state: "answered", answers: ["Banana", "Red, Blue"] })).toBe(
      "Fruit: Banana · Colors: Red, Blue",
    );
    expect(choiceAnsweredSummary({ ...choice, state: "declined" })).toBe("Dismissed");
  });

  test("the timeline hides a pending choice and resolves it to one Answered line", () => {
    expect(buildTimeline([choiceEvent("toolu_1", "pending")])).toEqual([]);
    const [answered] = buildTimeline([choiceEvent("toolu_1", "answered", ["Banana", "Red, Teal"])]);
    expect(answered).toMatchObject({
      type: "activity",
      id: "toolu_1",
      title: "Answered",
      detail: "Fruit: Banana · Colors: Red, Teal",
      tone: "success",
    });
    const [declined] = buildTimeline([choiceEvent("toolu_1", "declined")]);
    expect(declined).toMatchObject({ title: "Choice dismissed", tone: "neutral" });
    const [unanswered] = buildTimeline([choiceEvent("toolu_1", "unanswered")]);
    expect(unanswered).toMatchObject({ title: "Not answered", detail: "Which fruit?" });
    expect(latestPendingChoice([choiceEvent("toolu_1", "unanswered")])).toBeNull();
  });
});

describe("one-question-at-a-time steps", () => {
  const wizard: ConversationChoice = {
    kind: "AskUserQuestion",
    state: "pending",
    questions: [
      { question: "A?", options: [{ label: "a1" }, { label: "a2" }] },
      { question: "B?", multi_select: true, options: [{ label: "b1" }, { label: "b2" }] },
      { question: "C?", options: [{ label: "c1" }, { label: "c2" }] },
    ],
  };

  test("review is the step after the last question and steps never leave the range", () => {
    expect(choiceReviewStep(wizard)).toBe(3);
    expect(nextChoiceStep(wizard, 0)).toBe(1);
    expect(nextChoiceStep(wizard, 2)).toBe(3);
    expect(nextChoiceStep(wizard, 3)).toBe(3);
    expect(clampChoiceStep(wizard, -4)).toBe(0);
    expect(clampChoiceStep(wizard, 9)).toBe(3);
    expect(clampChoiceStep(wizard, Number.NaN)).toBe(0);
  });

  test("unanswered questions follow the draft, including an empty Other", () => {
    let draft = emptyChoiceDraft(wizard);
    expect(unansweredChoiceQuestions(wizard, draft)).toEqual([0, 1, 2]);
    draft = toggleChoiceOption(wizard, draft, 0, 1);
    draft = toggleChoiceOther(wizard, draft, 2);
    expect(unansweredChoiceQuestions(wizard, draft)).toEqual([1, 2]);
    draft = setChoiceOtherText(draft, 2, "  Sea ");
    draft = toggleChoiceOption(wizard, draft, 1, 0);
    expect(unansweredChoiceQuestions(wizard, draft)).toEqual([]);
    // Changing an answer later replaces it and leaves the others alone.
    draft = toggleChoiceOption(wizard, draft, 0, 0);
    expect(buildChoiceAnswers(wizard, draft)).toEqual([
      { selected: [0], other: "" },
      { selected: [0], other: "" },
      { selected: [], other: "Sea" },
    ]);
  });

  test("answer text joins the chosen labels and the Other text for the review", () => {
    let draft = emptyChoiceDraft(wizard);
    expect(choiceDraftAnswerText(wizard.questions[1], draft[1])).toBe("");
    draft = toggleChoiceOption(wizard, draft, 1, 1);
    draft = toggleChoiceOption(wizard, draft, 1, 0);
    draft = toggleChoiceOther(wizard, draft, 1);
    expect(choiceDraftAnswerText(wizard.questions[1], draft[1])).toBe("");
    draft = setChoiceOtherText(draft, 1, "mine");
    expect(choiceDraftAnswerText(wizard.questions[1], draft[1])).toBe("b1, b2, mine");
  });

  test("the card height budget keeps the card under the header and never collapses", () => {
    expect(choiceCardMaxHeight({ windowHeight: 844, topChromeInset: 100, bottomInset: 34 })).toBe(590);
    expect(choiceCardMaxHeight({ windowHeight: 300, topChromeInset: 100, bottomInset: 34 })).toBe(200);
  });
});

describe("a pending choice the status has not caught up with", () => {
  test("stays the visible prompt whatever the Session status says", () => {
    // The surface no longer consults the Session status to show or answer the
    // card; only the transcript's pending state and the daemon's verdict count.
    const events = [choiceEvent("toolu_1", "pending")];
    expect(visiblePendingChoice(events, new Set())?.call_id).toBe("toolu_1");
  });

  test("goes away once the daemon says the question passed", () => {
    const events = [choiceEvent("toolu_1", "pending")];
    expect(visiblePendingChoice(events, new Set(["toolu_1"]))).toBeNull();
    expect(visiblePendingChoice(events, new Set(["toolu_0"]))?.call_id).toBe("toolu_1");
    expect(visiblePendingChoice([...events, choiceEvent("toolu_2", "pending")], new Set(["toolu_1"]))?.call_id).toBe("toolu_2");
  });

  test("still counts as waiting for the person after the card is hidden as passed", () => {
    const events = [choiceEvent("toolu_1", "pending")];
    expect(visiblePendingChoice(events, new Set(["toolu_1"]))).toBeNull();
    expect(awaitingChoiceAnswer(events)).toBe(true);
    expect(awaitingChoiceAnswer([choiceEvent("toolu_1", "answered", ["Apple", "Red"])])).toBe(false);
    expect(awaitingChoiceAnswer([])).toBe(false);
    expect(awaitingChoiceAnswer(undefined)).toBe(false);
  });

  test("recognises the daemon's gone code and nothing else", () => {
    expect(isChoiceGoneError(Object.assign(new Error("x"), { code: CHOICE_GONE_CODE }))).toBe(true);
    expect(isChoiceGoneError(Object.assign(new Error("x"), { code: "answer_choice_refused" }))).toBe(false);
    expect(isChoiceGoneError(new Error(CHOICE_GONE_CODE))).toBe(false);
    expect(isChoiceGoneError(undefined)).toBe(false);
  });
});

describe("a choice prompt the daemon read from the terminal", () => {
  const prompt = (questions: any[], extra: any = {}) => ({
    id: "choice-1",
    kind: "choice" as const,
    provider: "claude",
    title: "Question",
    questions,
    ...extra,
  });
  const fruit = {
    header: "Fruit",
    question: "Which fruit?",
    allows_other: true,
    options: [
      { number: 1, label: "Apple", description: "Red and crisp" },
      { number: 2, label: "Banana" },
    ],
  };
  const colors = {
    header: "Colors",
    question: "Which colors?",
    multi_select: true,
    options: [
      { number: 1, label: "Red" },
      { number: 2, label: "Green" },
      { number: 3, label: "Blue" },
    ],
  };

  test("two questions, one of them multi-select, answer by option index", () => {
    const shown = choiceFromPrompt(prompt([fruit, colors]));
    expect(shown?.questions.map((q) => [q.header, q.question, q.multi_select ?? false, q.options.length])).toEqual([
      ["Fruit", "Which fruit?", false, 2],
      ["Colors", "Which colors?", true, 3],
    ]);
    expect(shown?.questions[0].options[0]).toEqual({ label: "Apple", description: "Red and crisp" });
    expect(questionAllowsOther(shown!.questions[0])).toBe(true);
    expect(questionAllowsOther(shown!.questions[1])).toBe(false);
    let draft = emptyChoiceDraft(shown!);
    draft = toggleChoiceOption(shown!, draft, 0, 1);
    draft = toggleChoiceOption(shown!, draft, 1, 0);
    draft = toggleChoiceOption(shown!, draft, 1, 2);
    expect(buildChoiceAnswers(shown!, draft)).toEqual([
      { selected: [1], other: "" },
      { selected: [0, 2], other: "" },
    ]);
  });

  test("the daemon's Other flag decides the Other row, not the options", () => {
    const shown = choiceFromPrompt(prompt([{ ...fruit, allows_other: false }]));
    expect(toggleChoiceOther(shown!, emptyChoiceDraft(shown!), 0)[0].otherActive).toBe(false);
  });

  test("tabs that carry only a header are filled from the transcript's same tool call", () => {
    const headerOnly = prompt([{ header: "Fruit", question: "", options: [] }, { header: "Colors", question: "", options: [] }], {
      call_id: "toolu_1",
    });
    expect(choiceFromPrompt(headerOnly)).toBeNull();
    expect(choiceFromPrompt(headerOnly, { callId: "toolu_other", choice })).toBeNull();
    const filled = choiceFromPrompt(headerOnly, { callId: "toolu_1", choice });
    expect(filled?.questions.map((q) => q.question)).toEqual(["Which fruit?", "Which colors?"]);
    expect(filled?.questions[1].multi_select).toBe(true);
  });

  test("one tab with options and another without is filled, never half-shown", () => {
    const mixed = prompt([fruit, { header: "Colors", question: "", options: [] }], { call_id: "toolu_1" });
    expect(choiceFromPrompt(mixed)).toBeNull();
    const filled = choiceFromPrompt(mixed, { callId: "toolu_1", choice });
    expect(filled?.questions[0].question).toBe("Which fruit?");
    expect(filled?.questions[1].question).toBe("Which colors?");
  });

  test("a menu prompt or nothing is not a choice", () => {
    expect(choiceFromPrompt(undefined)).toBeNull();
    expect(
      choiceFromPrompt({ id: "p", kind: "plan", provider: "claude", title: "t", options: [{ number: 1, label: "Yes" }] }),
    ).toBeNull();
  });

  test("recognises the daemon's prompt gone code and nothing else", () => {
    expect(isPromptGoneError(Object.assign(new Error("x"), { code: PROMPT_GONE_CODE }))).toBe(true);
    expect(isPromptGoneError(Object.assign(new Error("x"), { code: "answer_prompt_refused" }))).toBe(false);
    expect(isPromptGoneError(Object.assign(new Error("x"), { code: "answer_choice_gone" }))).toBe(false);
    expect(isPromptGoneError(new Error(PROMPT_GONE_CODE))).toBe(false);
  });
});
