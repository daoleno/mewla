import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, Typography } from "../../constants/tokens";
import type { ConversationChoice } from "../../services/codexConversation";
import { outlinedSurface } from "../ui/outlinedSurface";
import { Icon } from "../icons/Icon";
import {
  buildChoiceAnswerPayload,
  choiceDraftAnswerText,
  choiceQuestionAnswered,
  choiceReviewStep,
  clampChoiceStep,
  emptyChoiceDraft,
  nextChoiceStep,
  questionAllowsOther,
  setChoiceOtherText,
  toggleChoiceOption,
  toggleChoiceOther,
  unansweredChoiceQuestions,
  type ChoiceAnswerPayload,
} from "./claudeChoiceModel";

interface ClaudeChoiceCardProps {
  callId: string;
  choice: ConversationChoice;
  /** The Session list confirms the prompt is on screen; only changes the copy. */
  live: boolean;
  /** The daemon is reachable. The daemon re-checks the screen before any key. */
  connected: boolean;
  chrome: TerminalThemeChrome;
  /** The tallest the card may grow before it scrolls instead of passing under the header. */
  maxHeight?: number;
  onSubmit(payload: ChoiceAnswerPayload): Promise<void>;
}

/**
 * One question at a time, modelled on Claude Code's own prompt: a tab per
 * question header, a final review step, single-select picks that move on, and
 * Other as an inline text row.
 */
export function ClaudeChoiceCard({
  callId,
  choice,
  live,
  connected,
  chrome,
  maxHeight,
  onSubmit,
}: ClaudeChoiceCardProps) {
  const [draft, setDraft] = useState(() => emptyChoiceDraft(choice));
  const [step, setStep] = useState(0);
  const [typing, setTyping] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const tabsRef = useRef<ScrollView>(null);
  const tabOffsets = useRef<Record<number, number>>({});

  useEffect(() => {
    setDraft(emptyChoiceDraft(choice));
    setStep(0);
    setTyping(false);
    setSending(false);
    setError(null);
    // A new tool call is a new prompt; the same call keeps the user's draft.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [callId]);

  const reviewStep = choiceReviewStep(choice);
  const current = clampChoiceStep(choice, step);
  const reviewing = current === reviewStep;
  const missing = useMemo(() => unansweredChoiceQuestions(choice, draft), [choice, draft]);
  const payload = useMemo(
    () => buildChoiceAnswerPayload(callId, choice, draft),
    [callId, choice, draft],
  );
  const editable = connected && !sending;
  const canSend = Boolean(payload) && editable;
  const showTabs = choice.questions.length > 1 && !typing;

  useEffect(() => {
    tabsRef.current?.scrollTo?.({ x: Math.max(0, (tabOffsets.current[current] ?? 0) - 24), animated: true });
  }, [current]);

  const goTo = (target: number) => {
    setTyping(false);
    setStep(clampChoiceStep(choice, target));
  };

  const submit = () => {
    if (!payload || !editable) {
      return;
    }
    setSending(true);
    setError(null);
    onSubmit(payload)
      .catch((reason: unknown) => {
        setError(reason instanceof Error && reason.message ? reason.message : "Could not send the answer.");
      })
      .finally(() => setSending(false));
  };

  const question = reviewing ? undefined : choice.questions[current];
  const answer = draft[current];
  const multi = Boolean(question?.multi_select);
  const allowsOther = question ? questionAllowsOther(question) : false;
  const questionAnswered = question ? choiceQuestionAnswered(question, answer) : false;

  const pickOption = (optionIndex: number) => {
    if (!question) {
      return;
    }
    setDraft((existing) => toggleChoiceOption(choice, existing, current, optionIndex));
    if (!multi) {
      goTo(nextChoiceStep(choice, current));
    }
  };

  const otherActive = Boolean(answer?.otherActive);

  return (
    <View
      accessibilityRole="summary"
      accessibilityLabel="Waiting for your choice"
      style={[
        styles.card,
        { backgroundColor: chrome.surface, borderColor: chrome.borderStrong },
        maxHeight != null ? { maxHeight } : null,
      ]}
    >
      <Text style={[styles.waiting, { color: chrome.textMuted }]} numberOfLines={1}>
        Waiting for your answer
      </Text>
      {showTabs ? (
        <ScrollView
          ref={tabsRef}
          horizontal
          accessibilityRole="tablist"
          showsHorizontalScrollIndicator={false}
          keyboardShouldPersistTaps="handled"
          style={styles.tabs}
          contentContainerStyle={styles.tabsContent}
        >
          {choice.questions.map((item, index) => {
            const done = choiceQuestionAnswered(item, draft[index]);
            const active = index === current;
            const name = item.header?.trim() || `Q${index + 1}`;
            return (
              <Pressable
                key={`${index}:${item.question}`}
                accessibilityRole="tab"
                accessibilityLabel={`${name}, ${done ? "answered" : "not answered"}`}
                accessibilityState={{ selected: active, disabled: sending }}
                disabled={sending}
                onLayout={(event) => {
                  tabOffsets.current[index] = event.nativeEvent.layout.x;
                }}
                onPress={() => goTo(index)}
                style={[styles.tab, tabColors(chrome, active)]}
              >
                <Icon
                  name={done ? "check" : "circle"}
                  size={done ? 13 : 11}
                  color={done ? chrome.accent : chrome.textSubtle}
                />
                <Text
                  style={[styles.tabText, { color: active ? chrome.text : chrome.textMuted }]}
                  numberOfLines={1}
                >
                  {name}
                </Text>
              </Pressable>
            );
          })}
          <Pressable
            accessibilityRole="tab"
            accessibilityLabel={`Review, ${missing.length ? `${missing.length} not answered` : "ready to send"}`}
            accessibilityState={{ selected: reviewing, disabled: sending }}
            disabled={sending}
            onLayout={(event) => {
              tabOffsets.current[reviewStep] = event.nativeEvent.layout.x;
            }}
            onPress={() => goTo(reviewStep)}
            style={[styles.tab, tabColors(chrome, reviewing)]}
          >
            <Icon name="checks" size={14} color={missing.length ? chrome.textSubtle : chrome.accent} />
            <Text style={[styles.tabText, { color: reviewing ? chrome.text : chrome.textMuted }]} numberOfLines={1}>
              Review
            </Text>
          </Pressable>
        </ScrollView>
      ) : null}

      {question ? (
        <>
          <ScrollView
            style={styles.body}
            contentContainerStyle={styles.bodyContent}
            keyboardShouldPersistTaps="handled"
          >
            {choice.questions.length === 1 && question.header ? (
              <Text style={[styles.header, { color: chrome.textMuted }]} numberOfLines={1}>
                {question.header}
              </Text>
            ) : null}
            <Text accessibilityRole="header" style={[styles.questionText, { color: chrome.text }]}>
              {question.question}
            </Text>
            {multi && !typing ? (
              <Text style={[styles.hint, { color: chrome.textSubtle }]}>Choose any</Text>
            ) : null}
            {typing
              ? null
              : question.options.map((option, optionIndex) => {
                  const selected = answer?.selected.includes(optionIndex) ?? false;
                  return (
                    <Pressable
                      key={`${optionIndex}:${option.label}`}
                      accessibilityRole={multi ? "checkbox" : "radio"}
                      accessibilityLabel={[option.label, option.description].filter(Boolean).join(", ")}
                      accessibilityState={{ checked: selected, disabled: !editable }}
                      aria-checked={selected}
                      disabled={!editable}
                      onPress={() => pickOption(optionIndex)}
                      style={({ pressed }) => [
                        styles.option,
                        { backgroundColor: selected ? chrome.accentSoft : "transparent" },
                        pressed ? { backgroundColor: chrome.surfaceActive } : null,
                      ]}
                    >
                      <Marker chrome={chrome} multi={multi} selected={selected} number={optionIndex + 1} />
                      <View style={styles.optionCopy}>
                        <Text style={[styles.optionLabel, { color: chrome.text }]}>{option.label}</Text>
                        {option.description ? (
                          <Text style={[styles.optionDescription, { color: chrome.textMuted }]} numberOfLines={3}>
                            {option.description}
                          </Text>
                        ) : null}
                        {option.preview && selected ? (
                          <Text
                            style={[
                              styles.preview,
                              { color: chrome.text, backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
                            ]}
                          >
                            {option.preview}
                          </Text>
                        ) : null}
                      </View>
                    </Pressable>
                  );
                })}
          </ScrollView>
          {allowsOther ? (
            <View
              style={[
                styles.option,
                styles.otherRow,
                { backgroundColor: otherActive ? chrome.accentSoft : chrome.surfaceMuted },
              ]}
            >
              <Pressable
                accessibilityRole={multi ? "checkbox" : "radio"}
                accessibilityLabel="Other"
                accessibilityState={{ checked: otherActive, disabled: !editable }}
                aria-checked={otherActive}
                disabled={!editable}
                hitSlop={8}
                onPress={() => setDraft((existing) => toggleChoiceOther(choice, existing, current))}
              >
                <Marker
                  chrome={chrome}
                  multi={multi}
                  selected={otherActive}
                  number={question.options.length + 1}
                />
              </Pressable>
              <TextInput
                key={current}
                accessibilityLabel={`Other answer for ${question.header || question.question}`}
                editable={editable}
                placeholder="Type something…"
                placeholderTextColor={chrome.textSubtle}
                value={answer?.other ?? ""}
                onFocus={() => {
                  setTyping(true);
                  if (!otherActive) {
                    setDraft((existing) => toggleChoiceOther(choice, existing, current));
                  }
                }}
                onBlur={() => setTyping(false)}
                onChangeText={(text) => setDraft((existing) => setChoiceOtherText(existing, current, text))}
                onSubmitEditing={() => {
                  if (!multi && answer?.other.trim()) {
                    goTo(nextChoiceStep(choice, current));
                  }
                }}
                returnKeyType={multi ? "done" : "next"}
                blurOnSubmit
                style={[styles.otherInput, { color: chrome.text }]}
              />
            </View>
          ) : null}
        </>
      ) : (
        <ScrollView
          style={styles.body}
          contentContainerStyle={styles.bodyContent}
          keyboardShouldPersistTaps="handled"
        >
          <Text accessibilityRole="header" style={[styles.questionText, { color: chrome.text }]}>
            Review your answers
          </Text>
          {choice.questions.map((item, index) => {
            const text = choiceDraftAnswerText(item, draft[index]);
            return (
              <Pressable
                key={`${index}:${item.question}`}
                accessibilityRole="button"
                accessibilityLabel={`${item.header?.trim() || item.question}, ${text || "not answered"}`}
                disabled={sending}
                onPress={() => goTo(index)}
                style={({ pressed }) => [
                  styles.option,
                  pressed ? { backgroundColor: chrome.surfaceActive } : null,
                ]}
              >
                <Icon
                  name={text ? "check" : "circle"}
                  size={text ? 16 : 14}
                  color={text ? chrome.accent : chrome.seal}
                />
                <View style={styles.optionCopy}>
                  <Text style={[styles.optionDescription, { color: chrome.textMuted }]} numberOfLines={2}>
                    {item.question}
                  </Text>
                  <Text
                    style={[styles.optionLabel, { color: text ? chrome.text : chrome.seal }]}
                    numberOfLines={3}
                  >
                    {text || "Not answered"}
                  </Text>
                </View>
                <Icon name="chevron-right" size={14} color={chrome.textSubtle} />
              </Pressable>
            );
          })}
        </ScrollView>
      )}

      {error ? (
        <Text style={[styles.status, { color: chrome.danger }]} numberOfLines={3}>
          {error}
        </Text>
      ) : !connected ? (
        <Text style={[styles.status, { color: chrome.textMuted }]} numberOfLines={2}>
          Reconnecting to answer…
        </Text>
      ) : !live ? (
        <Text style={[styles.status, { color: chrome.textMuted }]} numberOfLines={2}>
          Not confirmed in the Terminal yet; it is checked when you send.
        </Text>
      ) : null}

      <View style={styles.actions}>
        {current > 0 ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Previous question"
            disabled={sending}
            onPress={() => goTo(current - 1)}
            style={({ pressed }) => [styles.back, pressed ? { backgroundColor: chrome.surfaceActive } : null]}
          >
            <Icon name="chevron-left" size={14} color={chrome.accent} />
            <Text style={[styles.backText, { color: chrome.accent }]}>Previous</Text>
          </Pressable>
        ) : (
          <View />
        )}
        {reviewing ? (
          missing.length ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`Answer ${missing.length} more`}
              disabled={sending}
              onPress={() => goTo(missing[0])}
              style={[styles.primary, { borderColor: chrome.seal, borderWidth: 1 }]}
            >
              <Text style={[styles.primaryText, { color: chrome.seal }]}>
                {missing.length === 1 ? "1 question left" : `${missing.length} questions left`}
              </Text>
            </Pressable>
          ) : (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Send answer"
              accessibilityState={{ disabled: !canSend, busy: sending }}
              disabled={!canSend}
              onPress={submit}
              style={[styles.primary, { backgroundColor: canSend ? chrome.seal : chrome.disabledSurface }]}
            >
              {sending ? <ActivityIndicator size="small" color={chrome.onSeal} /> : null}
              <Text style={[styles.primaryText, { color: canSend ? chrome.onSeal : chrome.textSubtle }]}>
                {sending ? "Sending…" : "Submit"}
              </Text>
            </Pressable>
          )
        ) : (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Next question"
            accessibilityState={{ disabled: !questionAnswered || sending }}
            disabled={!questionAnswered || sending}
            onPress={() => goTo(nextChoiceStep(choice, current))}
            style={[
              styles.primary,
              { backgroundColor: questionAnswered && !sending ? chrome.accentSoft : chrome.disabledSurface },
            ]}
          >
            <Text
              style={[
                styles.primaryText,
                { color: questionAnswered && !sending ? chrome.text : chrome.textSubtle },
              ]}
            >
              {current === reviewStep - 1 ? "Review" : "Next"}
            </Text>
            <Icon
              name="chevron-right"
              size={14}
              color={questionAnswered && !sending ? chrome.text : chrome.textSubtle}
            />
          </Pressable>
        )}
      </View>
    </View>
  );
}

function tabColors(chrome: TerminalThemeChrome, active: boolean) {
  return {
    backgroundColor: active ? chrome.accentSoft : "transparent",
    borderColor: active ? chrome.accent : chrome.border,
  };
}

function Marker({
  chrome,
  multi,
  selected,
  number,
}: {
  chrome: TerminalThemeChrome;
  multi: boolean;
  selected: boolean;
  number: number;
}) {
  if (multi) {
    return (
      <View style={styles.marker}>
        <Icon
          name={selected ? "checkbox" : "square"}
          size={18}
          color={selected ? chrome.accent : chrome.textSubtle}
        />
      </View>
    );
  }
  return (
    <View
      style={[
        styles.marker,
        styles.numberMarker,
        {
          backgroundColor: selected ? chrome.accent : "transparent",
          borderColor: selected ? chrome.accent : chrome.border,
        },
      ]}
    >
      <Text style={[styles.number, { color: selected ? chrome.surface : chrome.textMuted }]}>{number}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    marginHorizontal: 12,
    marginBottom: 8,
    ...outlinedSurface(8),
    padding: 8,
    gap: 6,
    flexShrink: 1,
    overflow: "hidden",
  },
  tabs: {
    flexGrow: 0,
    flexShrink: 0,
  },
  tabsContent: {
    gap: 6,
    paddingRight: 4,
  },
  tab: {
    minHeight: 32,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    borderWidth: 1,
    borderRadius: 16,
    paddingHorizontal: 10,
    maxWidth: 160,
  },
  tabText: {
    ...TypeScale.caption,
    fontFamily: Typography.chatFontMedium,
    flexShrink: 1,
  },
  body: {
    flexGrow: 0,
    flexShrink: 1,
    minHeight: 0,
  },
  bodyContent: {
    gap: 2,
    paddingVertical: 2,
  },
  header: {
    ...TypeScale.micro,
  },
  waiting: {
    ...TypeScale.micro,
    paddingHorizontal: 4,
  },
  questionText: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 14,
    lineHeight: 20,
    letterSpacing: 0,
    paddingHorizontal: 4,
    paddingBottom: 4,
  },
  hint: {
    ...TypeScale.micro,
    paddingHorizontal: 4,
    paddingBottom: 2,
  },
  option: {
    minHeight: 44,
    borderRadius: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 8,
    paddingVertical: 6,
  },
  otherRow: {
    flexShrink: 0,
  },
  marker: {
    width: 22,
    height: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  numberMarker: {
    borderWidth: 1,
    borderRadius: 11,
  },
  number: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 12,
    lineHeight: 16,
    letterSpacing: 0,
  },
  optionCopy: {
    flex: 1,
    minWidth: 0,
    gap: 1,
  },
  optionLabel: {
    ...TypeScale.label,
    fontFamily: Typography.chatFontMedium,
  },
  optionDescription: {
    ...TypeScale.caption,
  },
  preview: {
    marginTop: 4,
    ...outlinedSurface(6),
    paddingHorizontal: 8,
    paddingVertical: 6,
    fontFamily: Typography.chatMonoFont,
    fontSize: 12,
    lineHeight: 16,
    letterSpacing: 0,
  },
  otherInput: {
    flex: 1,
    minWidth: 0,
    minHeight: 28,
    padding: 0,
    fontFamily: Typography.chatFont,
    fontSize: 14,
    lineHeight: 20,
  },
  status: {
    ...TypeScale.caption,
    paddingHorizontal: 4,
  },
  actions: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 8,
  },
  back: {
    minHeight: 40,
    borderRadius: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 2,
    paddingHorizontal: 8,
  },
  backText: {
    ...TypeScale.label,
    fontFamily: Typography.chatFontMedium,
  },
  primary: {
    minHeight: 40,
    borderRadius: 20,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 16,
  },
  primaryText: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 14,
    lineHeight: 18,
    letterSpacing: 0,
  },
});
