import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View, type ScrollViewInstance,
} from "react-native";
import { TypeScale, Typography, useAppColors, type AppColors } from "../../constants/tokens";
import type { ConversationChoice } from "../../services/codexConversation";
import { Button } from "../ui/Button";
import { InlineNotice } from "../ui/InlineNotice";
import { outlinedSurface } from "../ui/outlinedSurface";
import { Icon } from "../icons/Icon";
import {
  buildChoiceAnswers,
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
  type ChoiceAnswer,
} from "./claudeChoiceModel";

interface ClaudeChoiceCardProps {
  /** The same question set keeps the person's draft; a new one starts over. */
  promptKey: string;
  choice: ConversationChoice;
  /** The daemon is reachable. The daemon re-checks the screen before any key. */
  connected: boolean;
  /** The tallest the card may grow before it scrolls instead of passing under the header. */
  maxHeight?: number;
  onSubmit(answers: ChoiceAnswer[]): Promise<void>;
}

/**
 * One question at a time, modelled on Claude Code's own prompt: a tab per
 * question header, a final review step, single-select picks that move on, and
 * Other as an inline text row.
 */
export function ClaudeChoiceCard({
  promptKey,
  choice,
  connected,
  maxHeight,
  onSubmit,
}: ClaudeChoiceCardProps) {
  const colors = useAppColors();
  const [draft, setDraft] = useState(() => emptyChoiceDraft(choice));
  const [step, setStep] = useState(0);
  const [typing, setTyping] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const tabsRef = useRef<ScrollViewInstance>(null);
  const tabOffsets = useRef<Record<number, number>>({});

  useEffect(() => {
    setDraft(emptyChoiceDraft(choice));
    setStep(0);
    setTyping(false);
    setSending(false);
    setError(null);
    // A new question set starts over; the same one keeps the user's draft.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [promptKey]);

  const reviewStep = choiceReviewStep(choice);
  const current = clampChoiceStep(choice, step);
  const reviewing = current === reviewStep;
  const missing = useMemo(() => unansweredChoiceQuestions(choice, draft), [choice, draft]);
  const answers = useMemo(() => buildChoiceAnswers(choice, draft), [choice, draft]);
  const editable = connected && !sending;
  const canSend = Boolean(answers) && editable;
  const showTabs = choice.questions.length > 1 && !typing;

  useEffect(() => {
    tabsRef.current?.scrollTo?.({ x: Math.max(0, (tabOffsets.current[current] ?? 0) - 24), animated: true });
  }, [current]);

  const goTo = (target: number) => {
    setTyping(false);
    setStep(clampChoiceStep(choice, target));
  };

  const submit = () => {
    if (!answers || !editable) {
      return;
    }
    setSending(true);
    setError(null);
    onSubmit(answers)
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
        { backgroundColor: colors.bgSurface, borderColor: colors.borderStrong },
        maxHeight != null ? { maxHeight } : null,
      ]}
    >
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
                style={[styles.tab, tabColors(colors, active)]}
              >
                <Icon
                  name={done ? "check" : "circle"}
                  size={done ? 13 : 11}
                  color={done ? colors.accent : colors.textTertiary}
                />
                <Text
                  style={[styles.tabText, { color: active ? colors.textPrimary : colors.textSecondary }]}
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
            style={[styles.tab, tabColors(colors, reviewing)]}
          >
            <Icon name="checks" size={14} color={missing.length ? colors.textTertiary : colors.accent} />
            <Text style={[styles.tabText, { color: reviewing ? colors.textPrimary : colors.textSecondary }]} numberOfLines={1}>
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
              <Text style={[styles.header, { color: colors.textSecondary }]} numberOfLines={1}>
                {question.header}
              </Text>
            ) : null}
            <Text accessibilityRole="header" style={[styles.questionText, { color: colors.textPrimary }]}>
              {question.question}
            </Text>
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
                        { backgroundColor: selected ? colors.accentSoft : "transparent" },
                        pressed ? { backgroundColor: colors.surfaceActive } : null,
                      ]}
                    >
                      <Marker colors={colors} multi={multi} selected={selected} number={optionIndex + 1} />
                      <View style={styles.optionCopy}>
                        <Text style={[styles.optionLabel, { color: colors.textPrimary }]}>{option.label}</Text>
                        {option.description ? (
                          <Text style={[styles.optionDescription, { color: colors.textSecondary }]} numberOfLines={3}>
                            {option.description}
                          </Text>
                        ) : null}
                        {option.preview && selected ? (
                          <Text
                            style={[
                              styles.preview,
                              { color: colors.textPrimary, backgroundColor: colors.surfaceSubtle, borderColor: colors.border },
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
                { backgroundColor: otherActive ? colors.accentSoft : colors.surfaceSubtle },
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
                  colors={colors}
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
                placeholderTextColor={colors.textTertiary}
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
                style={[styles.otherInput, { color: colors.textPrimary }]}
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
          <Text accessibilityRole="header" style={[styles.questionText, { color: colors.textPrimary }]}>
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
                  pressed ? { backgroundColor: colors.surfaceActive } : null,
                ]}
              >
                <Icon
                  name={text ? "check" : "circle"}
                  size={text ? 16 : 14}
                  color={text ? colors.accent : colors.seal}
                />
                <View style={styles.optionCopy}>
                  <Text style={[styles.optionDescription, { color: colors.textSecondary }]} numberOfLines={2}>
                    {item.question}
                  </Text>
                  <Text
                    style={[styles.optionLabel, { color: text ? colors.textPrimary : colors.seal }]}
                    numberOfLines={3}
                  >
                    {text || "Not answered"}
                  </Text>
                </View>
                <Icon name="chevron-right" size={14} color={colors.textTertiary} />
              </Pressable>
            );
          })}
        </ScrollView>
      )}

      {error ? (
        <InlineNotice tone="danger" title="Could not send" detail={error} detailNumberOfLines={3} />
      ) : !connected ? (
        <InlineNotice busy title="Reconnecting to answer…" />
      ) : null}

      <View style={styles.actions}>
        {current > 0 ? (
          <Button
            label="Previous"
            accessibilityLabel="Previous question"
            icon="chevron-left"
            variant="plain"
            size="sm"
            disabled={sending}
            onPress={() => goTo(current - 1)}
          />
        ) : (
          <View />
        )}
        {reviewing ? (
          missing.length ? (
            <Button
              label={missing.length === 1 ? "1 question left" : `${missing.length} questions left`}
              accessibilityLabel={`Answer ${missing.length} more`}
              variant="outlined"
              size="sm"
              disabled={sending}
              onPress={() => goTo(missing[0])}
            />
          ) : (
            <Button
              label={sending ? "Sending…" : "Submit"}
              accessibilityLabel="Send answer"
              variant="seal"
              size="sm"
              loading={sending}
              disabled={!canSend}
              onPress={submit}
            />
          )
        ) : (
          <Button
            label={current === reviewStep - 1 ? "Review" : "Next"}
            accessibilityLabel="Next question"
            variant="tinted"
            size="sm"
            disabled={!questionAnswered || sending}
            onPress={() => goTo(nextChoiceStep(choice, current))}
          />
        )}
      </View>
    </View>
  );
}

function tabColors(colors: AppColors, active: boolean) {
  return {
    backgroundColor: active ? colors.accentSoft : "transparent",
    borderColor: active ? colors.accent : colors.border,
  };
}

function Marker({
  colors,
  multi,
  selected,
  number,
}: {
  colors: AppColors;
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
          color={selected ? colors.accent : colors.textTertiary}
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
          backgroundColor: selected ? colors.accent : "transparent",
          borderColor: selected ? colors.accent : colors.border,
        },
      ]}
    >
      <Text style={[styles.number, { color: selected ? colors.bgSurface : colors.textSecondary }]}>{number}</Text>
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
  questionText: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 14,
    lineHeight: 20,
    letterSpacing: 0,
    paddingHorizontal: 4,
    paddingBottom: 4,
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
  actions: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 8,
  },
});
