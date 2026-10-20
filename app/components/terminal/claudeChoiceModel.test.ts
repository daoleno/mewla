import { describe, expect, test } from "bun:test";
import {
  normalizeCodexConversation,
  type CodexConversationEvent,
  type ConversationChoice,
} from "../../services/codexConversation";
import {
  buildChoiceAnswerPayload,
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
  setChoiceOtherText,
  toggleChoiceOption,
  toggleChoiceOther,
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
    expect(buildChoiceAnswerPayload("toolu_1", choice, draft)).toBeNull();
    draft = setChoiceOtherText(draft, 1, " Mauve\n紫 ");
    expect(buildChoiceAnswerPayload("toolu_1", choice, draft)).toEqual({
      call_id: "toolu_1",
      answers: [{ selected: [1] }, { selected: [0, 2], other: "Mauve 紫" }],
    });
  });

  test("single-select Other replaces the option", () => {
    let draft = toggleChoiceOption(choice, emptyChoiceDraft(choice), 0, 0);
    draft = toggleChoiceOther(choice, draft, 0);
    draft = setChoiceOtherText(draft, 0, "Cherry");
    draft = toggleChoiceOption(choice, draft, 1, 1);
    expect(buildChoiceAnswerPayload("toolu_1", choice, draft)?.answers[0]).toEqual({
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
    expect(buildChoiceAnswerPayload("call", wizard, draft)?.answers).toEqual([
      { selected: [0] },
      { selected: [0] },
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
