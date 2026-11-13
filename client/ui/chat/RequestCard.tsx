import { computed, signal, Text, untracked, View } from "nsh";
import type { Signal, ViewRef } from "nsh";
import { clock } from "../calendar/clock";
import { sealedOfAnswered } from "../../core/timeline/answered";
import type { AnsweredTimelineItem } from "../../core/timeline/types";
import { Button, Input } from "nsh/base";
import type { ConversationChoice } from "../../core/conversation/conversation";
import { answerChoice, answerPrompt, decideApproval } from "../../core/conversation/turn";
import {
  buildChoiceAnswers, choiceDraftAnswerText, choiceQuestionAnswered, choiceReviewStep, emptyChoiceDraft, isChoiceGoneError, isPromptGoneError,
  latestPendingChoice, nextChoiceStep, questionAllowsOther, setChoiceOtherText, toggleChoiceOption, toggleChoiceOther, unansweredChoiceQuestions,
  type ChoiceAnswer,
} from "../../core/timeline/claudeChoiceModel";
import type { CodexConversationEvent } from "../../core/conversation/conversation";
import type { TerminalActionPromptOption } from "../../core/session/terminalActionPrompt";
import {
  openCardKey, optionParts, promptParts, requestCardsOf, sealAction, sealedInThread, type RequestCard as Card, type RequestThread, type SealedRequest,
} from "../../core/timeline/requestCards";
import { AgentIcon } from "../icons/AgentIcon";
import { Icon } from "../kit/Icon";
import { brainChoice, brainHost, sealedRequests, sealRequest, tray } from "../tray/state";
import { connection, current, currentTransport, workers } from "../session";
import { BusyLabel, delayed, RING, Spinner } from "../kit/wait";
import { haptic } from "../shell/haptics";

/**
 * A thread's request cards (soft-final SPEC §1.1, §1.2, §11 Request card): the open requests of
 * Brain's thread or of one Session, from the same model as the tray, so a row and its card are one
 * object; a Session's own pending question (its transcript) fills a terminal question's options.
 */
export function threadRequests(thread: RequestThread, events?: () => readonly CodexConversationEvent[] | undefined) {
  return computed(() => {
    const workerId = thread.brain ? "" : thread.workerId;
    const pending = events && workerId ? latestPendingChoice(events()) : null;
    return requestCardsOf({
      requests: tray().needs,
      workers: workers(),
      brainHost: brainHost(),
      brainChoice: brainChoice(),
      transcriptChoice: pending?.call_id ? { workerId, callId: pending.call_id, choice: pending.choice } : null,
    }, thread);
  });
}

/** Requests answered here that this thread shows as quiet lines (the five newest). */
export function threadSealed(thread: RequestThread, durable?: () => ReadonlySet<string>) {
  // A line the thread already holds as its own answered row (kept by the computer) is not repeated at the foot.
  return computed(() => {
    const held = durable?.();
    return sealedRequests().filter((s) => sealedInThread(s, thread) && !held?.has(s.key)).slice(-5);
  });
}

/** The card keys a thread's own answered rows (WIRE §2) seal. */
export function durableAnswered(items: () => readonly { type: string }[]) {
  return computed(() => new Set(items().flatMap((item) => (item.type === "answered" ? (item as AnsweredTimelineItem).keys : []))));
}

/** What the person did with a card, for the thread to say ("That question has already passed."). */
export type RequestNotice = (text: string) => void;

const PASSED = "This request has already passed.";

function sealedOf(card: Card, verb: string, answer: string, mono?: boolean): SealedRequest {
  return { key: card.key, verb, answer, mono, title: card.title, source: card.source, brain: card.inBrain, workerId: card.sourceWorkerId };
}

/** The top line: a red dot and the kind, the source at the right (it opens the Session, or Brain's terminal). */
function TopLine(props: { card: () => Card; onSource?: (card: Card) => void }) {
  const card = props.card;
  return (
    <View class="min-h-[24px] flex-row items-center gap-2">
      <View class="size-2 rounded-full bg-seal" />
      <Text class="flex-1 text-caption font-semibold line-clamp-1">{card().label}</Text>
      {props.onSource && card().sourceWorkerId ? (
        <Button testID="request-source" accessibilityLabel={card().terminal ? "Open Brain's terminal" : `Open ${card().source}`} onPress={() => props.onSource?.(untracked(card))}
          class="-my-2.5 min-h-[44px] max-w-[60%] flex-row items-center gap-1 active:opacity-70">
          {card().agent && !card().terminal ? <AgentIcon kind={card().agent!} flavor={card().flavor ?? "shell"} size={14} /> : null}
          <Text class="shrink text-caption text-muted-foreground line-clamp-1">{card().terminal ? "Terminal" : card().source}</Text>
          <Icon name="chevron-right" size={14} tone="faint" />
        </Button>
      ) : null}
    </View>
  );
}

/** An option row's words (SPEC §11 Request card, "A row"): the label 17/24 up to two lines, the rest one soft 15/20 line. */
function OptionWords(props: { label: string; description?: string; brainAsks?: boolean }) {
  const parts = () => optionParts(props.label, props.description);
  return (
    <View class="min-w-0 flex-1 gap-0.5">
      {/* The recommendation is a 13 soft tag at the right of the label's first line, not a line of its own. */}
      <View class="flex-row items-start gap-2">
        <Text class="min-w-0 flex-1 text-body line-clamp-2">{parts().label}</Text>
        {parts().recommended ? <View class="pt-[3px]"><Text testID="option-pick" class="text-caption text-muted-foreground">{props.brainAsks ? "Brain's pick" : "Recommended"}</Text></View> : null}
      </View>
      {parts().detail ? <Text class="text-sub text-muted-foreground line-clamp-1">{parts().detail}</Text> : null}
    </View>
  );
}

/** A question: one step per question (54 pt rows with a ring), then the review and Submit. Made once per card key. */
function ChoiceBody(props: { card: Card; choice: ConversationChoice; connected: () => boolean; submit: (answers: ChoiceAnswer[]) => Promise<void>; onDone: (answer: string) => void; onGone: () => void }) {
  const choice = props.choice;
  const draft = signal(emptyChoiceDraft(choice));
  const step = signal(0);
  const sending = signal(false);
  const failure = signal("");
  const review = choiceReviewStep(choice);
  const pick = (q: number, o: number) => {
    draft.set(toggleChoiceOption(choice, draft(), q, o));
    if (!choice.questions[q]!.multi_select) step.set(nextChoiceStep(choice, q));
  };
  const submit = async () => {
    const answers = buildChoiceAnswers(choice, draft());
    if (!answers || sending()) return;
    sending.set(true);
    failure.set("");
    try {
      await props.submit(answers);
      haptic.impact("light");
      props.onDone(choice.questions.map((question, i) => choiceDraftAnswerText(question, draft()[i]!)).filter(Boolean).join(" · "));
    } catch (error) {
      if (isChoiceGoneError(error) || isPromptGoneError(error)) return props.onGone();
      failure.set(error instanceof Error && error.message ? error.message : "Couldn't send. Try again.");
    } finally {
      sending.set(false);
    }
  };
  const Question = (q: { index: number }) => {
    const question = choice.questions[q.index]!;
    const answer = () => draft()[q.index]!;
    return (
      <View class="gap-2">
        {question.header ? <Text class="text-caption font-semibold text-muted-foreground">{question.header}</Text> : null}
        <Text class="text-body font-semibold">{question.question}</Text>
        {question.multi_select ? <Text class="text-sub text-muted-foreground">Choose any</Text> : null}
        <View class="-mx-4">
          {question.options.map((option, o) => (
            <Button testID={`choice-option-${o}`} disabled={!props.connected()} onPress={() => pick(q.index, o)}
              class="min-h-[54px] flex-row items-center gap-3 border-t border-border px-4 py-2 active:bg-accent disabled:opacity-40">
              <OptionWords label={option.label} description={option.description} brainAsks={props.card.source === "Brain"} />
              <View class={answer().selected.includes(o) ? "size-[22px] items-center justify-center rounded-full bg-primary" : "size-[22px] rounded-full border-2 border-faint"}>
                {answer().selected.includes(o) ? <Icon name="check" size={14} tone="canvas" /> : null}
              </View>
            </Button>
          ))}
          {questionAllowsOther(question) ? (
            answer().otherActive ? (
              <View class="border-t border-border px-4 py-2">
                <Input testID="choice-other" value={answer().other} placeholder="Your answer" autoFocus
                  onChange={({ value }) => draft.set(setChoiceOtherText(draft(), q.index, value))}
                  class="min-h-[44px] rounded-(--radius-control) bg-muted px-3 text-body" />
              </View>
            ) : (
              <Button testID="choice-other-open" disabled={!props.connected()} onPress={() => draft.set(toggleChoiceOther(choice, draft(), q.index))}
                class="min-h-[54px] justify-center border-t border-border px-4 active:bg-accent disabled:opacity-40">
                <Text class="text-body text-muted-foreground">Other…</Text>
              </Button>
            )
          ) : null}
        </View>
      </View>
    );
  };
  const Review = () => (
    <View class="gap-2">
      <Text class="text-body font-semibold">Review your answers</Text>
      <View class="-mx-4">
        {choice.questions.map((question, i) => (
          <Button onPress={() => step.set(i)} class="min-h-[54px] justify-center border-t border-border px-4 py-2 active:bg-accent">
            <Text class="text-caption text-muted-foreground">{question.header || question.question}</Text>
            <Text class="text-body">{choiceDraftAnswerText(question, draft()[i]!) || "No answer yet"}</Text>
          </Button>
        ))}
      </View>
    </View>
  );
  const many = choice.questions.length > 1 || choice.questions.some((q) => q.multi_select);
  return (
    <View class="gap-3">
      {step() === review ? <Review /> : choice.questions.map((_, i) => (step() === i ? <Question index={i} /> : null))}
      {failure() ? <Text testID="request-failed" class="text-sub">{failure()}</Text> : null}
      {many || step() === review ? (
        <View class="flex-row items-center justify-between">
          <Button onPress={() => step.set(Math.max(0, step() - 1))} disabled={step() === 0} class="min-h-[44px] justify-center px-1 disabled:opacity-40">
            <Text class="text-body text-muted-foreground">Back</Text>
          </Button>
          {step() === review ? (
            <Button testID="choice-submit" onPress={() => void submit()} disabled={sending() || !props.connected() || unansweredChoiceQuestions(choice, draft()).length > 0}
              class={`h-[48px] items-center justify-center rounded-(--radius-control) bg-primary px-4 active:opacity-70 ${sending() ? "" : "disabled:opacity-40"}`}>
              <BusyLabel busy={sending()} label="Submit" tone="canvas" testID="request-ring"><Text class="text-body font-semibold text-primary-foreground">Submit</Text></BusyLabel>
            </Button>
          ) : (
            <Button testID="choice-next" onPress={() => step.set(nextChoiceStep(choice, step()))} disabled={!choiceQuestionAnswered(choice.questions[step()]!, draft()[step()]!)}
              class="min-h-[44px] justify-center px-1 disabled:opacity-40">
              <Text class="text-body font-semibold">{step() + 1 < review ? "Next" : "Review"}</Text>
            </Button>
          )}
        </View>
      ) : null}
    </View>
  );
}

/**
 * An approval or a terminal menu: the command, then Deny and Approve (a plain yes/no) or the menu's rows. The two
 * buttons always read Deny and Approve, whatever the agent calls them ("Allow", "Yes"): one act, one name (SPEC §11).
 */
function ActionBody(props: { card: Card; connected: () => boolean; onDone: (option: TerminalActionPromptOption) => void; onGone: () => void }) {
  const body = props.card.body as Extract<Card["body"], { kind: "action" }>;
  const prompt = body.prompt;
  const sending = signal("");
  const failed = signal(false);
  const ringFor = (() => {
    const on = delayed(() => Boolean(sending()));
    return () => (on() ? sending() : "");
  })();
  const choose = async (option: TerminalActionPromptOption) => {
    const transport = currentTransport();
    if (sending() || !transport) return;
    sending.set(option.id);
    failed.set(false);
    try {
      if (option.approvalOptionId && prompt.approvalId) await decideApproval(transport, body.workerId, prompt.approvalId, option.approvalOptionId);
      else if (option.number !== undefined && prompt.promptId) await answerPrompt(transport, body.workerId, prompt.promptId, { number: option.number });
      // Answered and acknowledged: the one light tick (SPEC §16.3 Haptics).
      haptic.impact("light");
      props.onDone(option);
    } catch (error) {
      if (isPromptGoneError(error)) return props.onGone();
      failed.set(true);
    } finally {
      sending.set("");
    }
  };
  const asked = () => promptParts(prompt.requestText ?? "", prompt.commandText);
  const question = () => {
    const text = asked().question || props.card.title;
    return text.trim() === asked().command ? "" : text;
  };
  const allow = prompt.options.find((option) => option.primary);
  const deny = prompt.options.find((option) => option.destructive);
  const yesNo = prompt.options.length === 2 && allow && deny && allow !== deny;
  return (
    <View class="gap-3">
      {/* The question at 17/600, the command in mono, what else the agent said in soft (SPEC §11 Request card). */}
      {/* A hook approval asks nothing but its command: then the command stands alone, not said twice. */}
      {question() ? <Text testID="request-question" class="text-body font-semibold">{question()}</Text> : null}
      {asked().command ? <View selectable><Text testID="request-command" class="font-mono text-code">{asked().command}</Text></View> : null}
      {asked().notes ? <Text class="text-sub text-muted-foreground line-clamp-3">{asked().notes}</Text> : null}
      {yesNo ? (
        <View class="flex-row gap-2">
          {/* Not optimistic (SPEC §16.2 pattern 8): the press, then the ring in the pressed button; the other is disabled. */}
          <Button testID={`prompt-option-${deny!.id}`} disabled={Boolean(sending()) || !props.connected()} onPress={() => void choose(deny!)}
            style={{ flex: 2 }} class={`h-[48px] items-center justify-center rounded-(--radius-control) bg-muted active:opacity-70 ${sending() === deny!.id ? "" : "disabled:opacity-40"}`}>
            <BusyLabel busy={sending() === deny!.id} label="Deny" testID="request-ring"><Text class="text-body font-semibold line-clamp-1">Deny</Text></BusyLabel>
          </Button>
          <Button testID={`prompt-option-${allow!.id}`} disabled={Boolean(sending()) || !props.connected()} onPress={() => void choose(allow!)}
            style={{ flex: 3 }} class={`h-[48px] items-center justify-center rounded-(--radius-control) bg-seal active:opacity-70 ${sending() === allow!.id ? "" : "disabled:opacity-40"}`}>
            <BusyLabel busy={sending() === allow!.id} label="Approve" tone="onSeal" testID="request-ring">
              <Icon name="check" size={18} tone="onSeal" />
              <Text class="text-body font-semibold text-seal-foreground line-clamp-1">Approve</Text>
            </BusyLabel>
          </Button>
        </View>
      ) : (
        <View class="-mx-4">
          {prompt.options.map((option) => (
            <Button testID={`prompt-option-${option.id}`} disabled={Boolean(sending()) || !props.connected()} onPress={() => void choose(option)}
              class={`min-h-[54px] flex-row items-center gap-3 border-t border-border px-4 py-2 active:bg-accent ${sending() === option.id ? "" : "disabled:opacity-40"}`}>
              <OptionWords label={option.label} description={option.description} brainAsks={props.card.source === "Brain"} />
              {/* The pressed row's ring takes its circle's place after `wait.quiet`. */}
              <View class="size-[22px] items-center justify-center">
                <If when={ringFor() === option.id} else={<View class="size-[22px] rounded-full border-2 border-faint" />}>
                  <Spinner size={RING.button} label={option.label} />
                </If>
              </View>
            </Button>
          ))}
        </View>
      )}
      {/* One meta line, the same height either way: the answer did not arrive, or the computer is away. */}
      {failed() ? <Text testID="request-failed" class="text-sub">{`Didn't reach ${current()?.name || "your computer"}`}</Text>
        : !props.connected() ? <Text testID="request-offline" class="text-sub text-muted-foreground">{`${current()?.name || "Your computer"} is offline`}</Text> : null}
    </View>
  );
}

/** One open request: the card the person answers here. */
function OpenCard(props: { card: Card; connected: () => boolean; onSource?: (card: Card) => void; onOpenTerminal: (workerId: string) => void; notice: RequestNotice; anchor?: (node: ViewRef) => void }) {
  const card = props.card;
  const gone = () => {
    const command = card.body.kind === "action" ? promptParts(card.body.prompt.requestText ?? "", card.body.prompt.commandText).command : "";
    sealRequest(sealedOf(card, "Not answered", command || card.title, Boolean(command)));
    props.notice(PASSED);
  };
  const submit = (answers: ChoiceAnswer[]) => {
    const transport = currentTransport();
    if (!transport || card.body.kind !== "choice") return Promise.reject(new Error("Your computer is offline."));
    const route = card.body.route;
    return route.via === "choice" ? answerChoice(transport, route.workerId, route.callId, answers) : answerPrompt(transport, route.workerId, route.promptId, { answers });
  };
  return (
    <View testID="request-card" ref={props.anchor} class="gap-3 rounded-(--radius-group) bg-card p-4">
      <TopLine card={() => card} onSource={props.onSource} />
      {card.body.kind === "choice" ? (
        <ChoiceBody card={card} choice={card.body.choice} connected={props.connected} submit={submit}
          onDone={(answer) => sealRequest(sealedOf(card, "You chose", answer))} onGone={gone} />
      ) : card.body.kind === "action" ? (
        <ActionBody card={card} connected={props.connected} onGone={gone}
          onDone={(option) => {
            const words = sealAction(card, option.label, Boolean(option.destructive));
            sealRequest(sealedOf(card, words.verb, words.answer, words.mono));
          }} />
      ) : (
        <View class="gap-3">
          <Text class="text-body font-semibold">{card.body.detail}</Text>
          <Button testID="request-open-terminal" onPress={() => props.onOpenTerminal((card.body as { workerId: string }).workerId)}
            class="h-[48px] items-center justify-center self-start rounded-(--radius-control) bg-primary px-4 active:opacity-70">
            <Text class="text-body font-semibold text-primary-foreground">Open terminal</Text>
          </Button>
        </View>
      )}
    </View>
  );
}

/** A stacked request under the open one: 52 pt, the dot, the ask, then the source with its 16 pt logo. Tap opens it. */
function StackedRow(props: { card: () => Card; onPress: () => void }) {
  const c = props.card;
  return (
    <Button testID="request-stacked" accessibilityLabel={`${c().ask ?? c().title}, ${c().source}`} onPress={props.onPress}
      class="min-h-[52px] flex-row items-center gap-2.5 rounded-2xl bg-card px-4 active:opacity-70">
      <View class="size-2 rounded-full bg-seal" />
      <Text testID="request-stacked-ask" class="min-w-0 flex-1 text-sub font-semibold line-clamp-1">{c().ask ?? c().title}</Text>
      <View class="max-w-[42%] flex-row items-center gap-1.5">
        {c().agent ? <AgentIcon kind={c().agent!} flavor={c().flavor} size={16} /> : c().source === "Brain" ? <Icon name="brain" size={16} tone="soft" /> : null}
        <Text class="min-w-0 shrink text-sub text-muted-foreground line-clamp-1">{c().source}</Text>
      </View>
    </Button>
  );
}

/** The answered line's 20 pt mark: a check for yes, an x-circle for Denied, a dash-circle for Not answered. */
export function sealedIcon(verb: string): "check-circle-fill" | "close-circle-fill" | "remove-circle" {
  return verb === "Not answered" ? "remove-circle" : verb === "Denied" ? "close-circle-fill" : "check-circle-fill";
}

/** An answered request's quiet line: a check, what was done, the answer. Tap shows the question again. */
function SealedLine(props: { sealed: () => SealedRequest; open: () => boolean; onToggle: () => void }) {
  const s = props.sealed;
  return (
    <Button testID="request-sealed" accessibilityLabel={`${s().verb}: ${s().answer}`} onPress={props.onToggle} class="gap-1 py-1.5 active:opacity-70">
      {/* The mock's answered line: a 20 pt check, then the caption over the answer (a command in mono). */}
      <View class="flex-row items-start gap-2.5">
        {/* The check means yes: "Denied" takes an x-circle, and "Not answered", which nobody did, a dash-circle (SPEC §1.1). */}
        <View class="pt-0.5"><Icon name={sealedIcon(s().verb)} size={20} tone="soft" /></View>
        <View class="min-w-0 flex-1">
          <Text class="text-caption text-muted-foreground">{s().verb}</Text>
          <Text class={s().mono ? "font-mono text-code text-foreground" : "text-sub font-semibold text-foreground"}>{s().answer}</Text>
        </View>
      </View>
      {props.open() ? <Text class="pl-[30px] text-sub text-muted-foreground">{`${s().title} · ${s().source}`}</Text> : null}
    </Button>
  );
}

/** An answered request's line where it was asked (WIRE §2): kept by the computer, so it survives a restart. */
export function AnsweredRow(props: { item: () => AnsweredTimelineItem }) {
  // Open is this row's (by id): a recycled cell showing another answered row shows it closed.
  const openId = signal<string | null>(null);
  const open = () => openId() === props.item().id;
  const sealed = () => sealedOfAnswered(props.item().record, (ms) => clock().format.time(ms, clock().zones.device));
  return (
    <View testID="request-answered" class="px-4">
      <SealedLine sealed={sealed} open={open} onToggle={() => openId.set(open() ? null : untracked(props.item).id)} />
    </View>
  );
}

/**
 * The requests at the foot of a thread (never docked above the composer, SPEC §1.4): the quiet lines
 * of what was answered here, then the open request and the others stacked under it. `picked` is
 * the card the person (or a tray row) chose to open; answering opens the next.
 */
export function RequestStack(props: {
  thread: RequestThread;
  cards: () => readonly Card[];
  picked: Signal<string>;
  onSource?: (card: Card) => void;
  onOpenTerminal: (workerId: string) => void;
  notice: RequestNotice;
  /** Keys the thread holds as answered rows: their sealed lines are in the thread, not the foot. */
  durable?: () => ReadonlySet<string>;
  /** The open card's view: the thread centres it (SPEC §3.1 cold start, a tray row). */
  anchor?: (node: ViewRef) => void;
}) {
  const connected = () => connection() === "connected";
  // The open card is made once per key (its draft survives every update of the request list).
  const openKey = computed(() => openCardKey(props.cards(), props.picked()));
  const openCard = () => props.cards().find((card) => card.key === untracked(openKey));
  const sealed = threadSealed(props.thread, props.durable);
  const shown = signal<ReadonlySet<string>>(new Set());
  const toggle = (key: string) => {
    const next = new Set(shown());
    if (next.has(key)) next.delete(key);
    else next.add(key);
    shown.set(next);
  };
  return (
    <View testID="request-stack" class="gap-2 px-4 pb-2">
      {sealed().map((s) => <SealedLine sealed={() => s} open={() => shown().has(s.key)} onToggle={() => toggle(s.key)} />)}
      {openKey() ? <OpenCard card={untracked(openCard)!} connected={connected} onSource={props.onSource} onOpenTerminal={props.onOpenTerminal} notice={props.notice} anchor={props.anchor} /> : null}
      {props.cards().filter((card) => card.key !== openKey()).map((card) => <StackedRow card={() => card} onPress={() => props.picked.set(card.key)} />)}
    </View>
  );
}
