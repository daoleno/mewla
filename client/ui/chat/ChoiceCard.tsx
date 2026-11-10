import { signal, Text, View } from "nsh";
import { Button, Input } from "nsh/base";
import type { ConversationChoice } from "../../core/conversation/conversation";
import {
  buildChoiceAnswers,
  choiceDraftAnswerText,
  choiceQuestionAnswered,
  choiceReviewStep,
  emptyChoiceDraft,
  isChoiceGoneError,
  nextChoiceStep,
  questionAllowsOther,
  setChoiceOtherText,
  toggleChoiceOption,
  toggleChoiceOther,
  unansweredChoiceQuestions,
  CHOICE_GONE_NOTICE,
  type ChoiceAnswer,
} from "../../core/timeline/claudeChoiceModel";

/**
 * The agent's question (AskUserQuestion) above the composer: one question at a
 * time like Claude Code's terminal, then a review of every answer, then Submit.
 * Made once per question set (the parent keys it by call id), so the draft
 * survives every update of the conversation while it is open.
 */
export function ChoiceCard(props: {
  choice: ConversationChoice;
  connected: () => boolean;
  submit: (answers: ChoiceAnswer[]) => Promise<void>;
  /** The question passed (answered elsewhere, or the agent moved on). */
  onGone: () => void;
}) {
  const choice = props.choice;
  const draft = signal(emptyChoiceDraft(choice));
  const step = signal(0);
  const sending = signal(false);
  const failure = signal("");
  const review = choiceReviewStep(choice);

  const pick = (q: number, o: number) => {
    draft.set(toggleChoiceOption(choice, draft(), q, o));
    if (!choice.questions[q].multi_select) step.set(nextChoiceStep(choice, q));
  };
  const submit = async () => {
    const answers = buildChoiceAnswers(choice, draft());
    if (!answers || sending()) return;
    sending.set(true);
    failure.set("");
    try {
      await props.submit(answers);
    } catch (error) {
      if (isChoiceGoneError(error)) return props.onGone();
      failure.set(error instanceof Error && error.message ? error.message : "Could not send the answer.");
    } finally {
      sending.set(false);
    }
  };

  const Question = (q: { index: number }) => {
    const question = choice.questions[q.index];
    const answer = () => draft()[q.index];
    return (
      <View class="gap-2">
        {question.header ? <Text class="text-xs font-semibold uppercase text-muted-foreground">{question.header}</Text> : null}
        <Text class="text-base font-semibold">{question.question}</Text>
        {question.multi_select ? <Text class="text-sm text-muted-foreground">Choose any</Text> : null}
        {question.options.map((option, o) => (
          <Button testID={`choice-option-${o}`} onPress={() => pick(q.index, o)}
            class={answer().selected.includes(o) ? "rounded-xl border border-primary bg-accent px-3 py-2.5" : "rounded-xl border border-border px-3 py-2.5 active:bg-accent"}>
            <Text class="text-base">{option.label}</Text>
            {option.description ? <Text class="text-sm text-muted-foreground">{option.description}</Text> : null}
          </Button>
        ))}
        {questionAllowsOther(question) ? (
          answer().otherActive ? (
            <Input testID="choice-other" value={answer().other} placeholder="Your answer" autoFocus
              onChange={({ value }) => draft.set(setChoiceOtherText(draft(), q.index, value))}
              class="rounded-xl border border-primary bg-card px-3 py-2.5 text-base" />
          ) : (
            <Button testID="choice-other-open" onPress={() => draft.set(toggleChoiceOther(choice, draft(), q.index))} class="rounded-xl border border-dashed border-border px-3 py-2.5 active:bg-accent">
              <Text class="text-base text-muted-foreground">Other…</Text>
            </Button>
          )
        ) : null}
      </View>
    );
  };

  const Review = () => (
    <View class="gap-2">
      <Text class="text-base font-semibold">Review your answers</Text>
      {choice.questions.map((question, i) => (
        <Button onPress={() => step.set(i)} class="rounded-xl border border-border px-3 py-2 active:bg-accent">
          <Text class="text-sm text-muted-foreground">{question.header || question.question}</Text>
          <Text class="text-base">{choiceDraftAnswerText(question, draft()[i]) || "No answer yet"}</Text>
        </Button>
      ))}
    </View>
  );

  return (
    <View testID="choice-card" class="mx-3 mb-2 gap-3 rounded-2xl border border-border bg-card p-3">
      {step() === review ? <Review /> : choice.questions.map((_, i) => (step() === i ? <Question index={i} /> : null))}
      {failure() ? <Text class="text-sm text-destructive">{failure()}</Text> : null}
      {!props.connected() ? <Text class="text-sm text-muted-foreground">Reconnecting to answer…</Text> : null}
      <View class="flex-row items-center justify-between">
        <Button onPress={() => step.set(Math.max(0, step() - 1))} disabled={step() === 0} class="px-2 py-2">
          <Text class={step() === 0 ? "text-base text-muted-foreground" : "text-base text-primary"}>Back</Text>
        </Button>
        {step() === review ? (
          <Button testID="choice-submit" onPress={() => void submit()} disabled={sending() || !props.connected() || unansweredChoiceQuestions(choice, draft()).length > 0}
            class="rounded-full bg-primary px-4 py-2 active:opacity-80">
            <Text class="text-base font-semibold text-primary-foreground">{sending() ? "Sending…" : "Submit"}</Text>
          </Button>
        ) : (
          <Button testID="choice-next" onPress={() => step.set(nextChoiceStep(choice, step()))} disabled={!choiceQuestionAnswered(choice.questions[step()], draft()[step()])} class="px-2 py-2">
            <Text class="text-base text-primary">{step() + 1 < review ? "Next" : "Review"}</Text>
          </Button>
        )}
      </View>
    </View>
  );
}

export { CHOICE_GONE_NOTICE };
