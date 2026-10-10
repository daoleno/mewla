import React, { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
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
  emptyChoiceDraft,
  questionAllowsOther,
  setChoiceOtherText,
  toggleChoiceOption,
  toggleChoiceOther,
  type ChoiceAnswerPayload,
} from "./claudeChoiceModel";

interface ClaudeChoiceCardProps {
  callId: string;
  choice: ConversationChoice;
  /** The live pane shows this prompt; answers can be sent. */
  live: boolean;
  chrome: TerminalThemeChrome;
  onSubmit(payload: ChoiceAnswerPayload): Promise<void>;
  onSwitchToTerminal?: () => void;
}

export function ClaudeChoiceCard({
  callId,
  choice,
  live,
  chrome,
  onSubmit,
  onSwitchToTerminal,
}: ClaudeChoiceCardProps) {
  const [draft, setDraft] = useState(() => emptyChoiceDraft(choice));
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setDraft(emptyChoiceDraft(choice));
    setSending(false);
    setError(null);
    // A new tool call is a new prompt; the same call keeps the user's draft.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [callId]);

  const payload = useMemo(
    () => buildChoiceAnswerPayload(callId, choice, draft),
    [callId, choice, draft],
  );
  const editable = live && !sending;

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

  return (
    <View
      accessibilityRole="summary"
      accessibilityLabel="Waiting for your choice"
      style={[styles.card, { backgroundColor: chrome.surface, borderColor: chrome.borderStrong }]}
    >
      <View style={styles.header}>
        <View style={[styles.badge, { backgroundColor: chrome.seal }]}>
          <Icon name="help" size={13} color={chrome.onSeal} />
        </View>
        <Text style={[styles.title, { color: chrome.text }]}>Waiting for your choice</Text>
      </View>

      {choice.questions.map((question, questionIndex) => {
        const answer = draft[questionIndex];
        const allowsOther = questionAllowsOther(question);
        const multi = Boolean(question.multi_select);
        return (
          <View key={`${questionIndex}:${question.question}`} style={styles.question}>
            <View style={styles.questionHeader}>
              {question.header ? (
                <Text style={[styles.chip, { color: chrome.textMuted, borderColor: chrome.border }]} numberOfLines={1}>
                  {question.header}
                </Text>
              ) : null}
              {multi ? (
                <Text style={[styles.hint, { color: chrome.textSubtle }]}>Choose any</Text>
              ) : null}
            </View>
            <Text style={[styles.questionText, { color: chrome.text }]}>{question.question}</Text>
            <View style={styles.options}>
              {question.options.map((option, optionIndex) => {
                const selected = answer?.selected.includes(optionIndex) ?? false;
                return (
                  <Pressable
                    key={`${optionIndex}:${option.label}`}
                    accessibilityRole={multi ? "checkbox" : "radio"}
                    accessibilityLabel={[option.label, option.description].filter(Boolean).join(", ")}
                    accessibilityState={{ checked: selected, disabled: !editable }}
                    aria-checked={selected}
                    disabled={!editable}
                    onPress={() => setDraft((current) => toggleChoiceOption(choice, current, questionIndex, optionIndex))}
                    style={({ pressed }) => [
                      styles.option,
                      {
                        backgroundColor: selected ? chrome.accentSoft : chrome.surfaceMuted,
                        borderColor: selected ? chrome.accent : chrome.border,
                      },
                      pressed ? { borderColor: chrome.focus } : null,
                    ]}
                  >
                    <Icon
                      name={selectionIcon(multi, selected)}
                      size={17}
                      color={selected ? chrome.accent : chrome.textSubtle}
                    />
                    <View style={styles.optionCopy}>
                      <Text style={[styles.optionLabel, { color: chrome.text }]}>{option.label}</Text>
                      {option.description ? (
                        <Text style={[styles.optionDescription, { color: chrome.textMuted }]}>
                          {option.description}
                        </Text>
                      ) : null}
                      {option.preview && selected ? (
                        <Text
                          style={[
                            styles.preview,
                            { color: chrome.text, backgroundColor: chrome.surface, borderColor: chrome.border },
                          ]}
                        >
                          {option.preview}
                        </Text>
                      ) : null}
                    </View>
                  </Pressable>
                );
              })}
              {allowsOther ? (
                <View
                  style={[
                    styles.option,
                    {
                      backgroundColor: answer?.otherActive ? chrome.accentSoft : chrome.surfaceMuted,
                      borderColor: answer?.otherActive ? chrome.accent : chrome.border,
                    },
                  ]}
                >
                  <Pressable
                    accessibilityRole={multi ? "checkbox" : "radio"}
                    accessibilityLabel="Other"
                    accessibilityState={{ checked: Boolean(answer?.otherActive), disabled: !editable }}
                    aria-checked={Boolean(answer?.otherActive)}
                    disabled={!editable}
                    hitSlop={8}
                    onPress={() => setDraft((current) => toggleChoiceOther(choice, current, questionIndex))}
                  >
                    <Icon
                      name={selectionIcon(multi, Boolean(answer?.otherActive))}
                      size={17}
                      color={answer?.otherActive ? chrome.accent : chrome.textSubtle}
                    />
                  </Pressable>
                  <TextInput
                    accessibilityLabel={`Other answer for ${question.header || question.question}`}
                    editable={editable}
                    placeholder="Other…"
                    placeholderTextColor={chrome.textSubtle}
                    value={answer?.other ?? ""}
                    onFocus={() => {
                      if (!answer?.otherActive) {
                        setDraft((current) => toggleChoiceOther(choice, current, questionIndex));
                      }
                    }}
                    onChangeText={(text) => setDraft((current) => setChoiceOtherText(current, questionIndex, text))}
                    returnKeyType="done"
                    blurOnSubmit
                    style={[styles.otherInput, { color: chrome.text }]}
                  />
                </View>
              ) : null}
            </View>
          </View>
        );
      })}

      {error ? (
        <Text style={[styles.status, { color: chrome.danger }]} numberOfLines={3}>
          {error}
        </Text>
      ) : !live ? (
        <Text style={[styles.status, { color: chrome.textMuted }]} numberOfLines={2}>
          Waiting for the prompt to appear in the Terminal…
        </Text>
      ) : null}

      <View style={styles.actions}>
        {onSwitchToTerminal ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Open Terminal"
            onPress={onSwitchToTerminal}
            style={({ pressed }) => [styles.secondary, pressed ? { backgroundColor: chrome.surfaceActive } : null]}
          >
            <Text style={[styles.secondaryText, { color: chrome.accent }]}>Terminal</Text>
          </Pressable>
        ) : null}
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Send answer"
          accessibilityState={{ disabled: !payload || !editable, busy: sending }}
          disabled={!payload || !editable}
          onPress={submit}
          style={[
            styles.submit,
            { backgroundColor: payload && editable ? chrome.seal : chrome.disabledSurface },
          ]}
        >
          {sending ? <ActivityIndicator size="small" color={chrome.onSeal} /> : null}
          <Text style={[styles.submitText, { color: payload && editable ? chrome.onSeal : chrome.textSubtle }]}>
            {sending ? "Sending…" : "Send answer"}
          </Text>
        </Pressable>
      </View>
    </View>
  );
}

function selectionIcon(multi: boolean, selected: boolean) {
  if (multi) {
    return selected ? "checkbox" : "square";
  }
  return selected ? "radio-on" : "circle";
}

const styles = StyleSheet.create({
  card: {
    marginHorizontal: 12,
    marginBottom: 8,
    ...outlinedSurface(8),
    padding: 10,
    gap: 10,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  badge: {
    width: 22,
    height: 22,
    borderRadius: 11,
    alignItems: "center",
    justifyContent: "center",
  },
  title: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 13,
    lineHeight: 18,
    letterSpacing: 0,
  },
  question: {
    gap: 6,
  },
  questionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  chip: {
    ...TypeScale.micro,
    ...outlinedSurface(4),
    paddingHorizontal: 6,
    paddingVertical: 1,
    overflow: "hidden",
  },
  hint: {
    ...TypeScale.micro,
  },
  questionText: {
    fontFamily: Typography.chatFont,
    fontSize: 14,
    lineHeight: 20,
    letterSpacing: 0,
  },
  options: {
    gap: 6,
  },
  option: {
    minHeight: 44,
    ...outlinedSurface(8),
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 9,
    paddingHorizontal: 10,
    paddingVertical: 9,
  },
  optionCopy: {
    flex: 1,
    minWidth: 0,
    gap: 2,
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
    minHeight: 24,
    padding: 0,
    fontFamily: Typography.chatFont,
    fontSize: 14,
    lineHeight: 20,
  },
  status: {
    ...TypeScale.caption,
  },
  actions: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 8,
  },
  secondary: {
    minHeight: 44,
    borderRadius: 8,
    justifyContent: "center",
    paddingHorizontal: 8,
  },
  secondaryText: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 12,
    lineHeight: 16,
    letterSpacing: 0,
  },
  submit: {
    minHeight: 40,
    borderRadius: 20,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 16,
  },
  submitText: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 14,
    lineHeight: 18,
    letterSpacing: 0,
  },
});
