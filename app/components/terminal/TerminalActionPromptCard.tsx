import React, {
  useEffect,
  useState,
} from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { TerminalThemePalette } from "../../constants/terminalThemes";
import { TypeScale, Typography } from "../../constants/tokens";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import { outlinedSurface } from "../ui/outlinedSurface";
import { Icon } from "../icons/Icon";

interface TerminalActionPromptCardProps {
  prompt: TerminalActionPrompt;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  /** Answers with the chosen option: its menu number, or the agent's own approval answer. */
  onAnswer(option: TerminalActionPrompt["options"][number]): Promise<void> | void;
}

export function TerminalActionPromptCard({
  prompt,
  chrome,
  onAnswer,
}: TerminalActionPromptCardProps) {
  const [sendingOptionId, setSendingOptionId] = useState<string | null>(null);
  const [sentOptionId, setSentOptionId] = useState<string | null>(null);
  const [failedOptionId, setFailedOptionId] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  useEffect(() => {
    setSendingOptionId(null);
    setSentOptionId(null);
    setFailedOptionId(null);
    setFailure(null);
  }, [prompt.id]);

  const handleOptionPress = (option: TerminalActionPrompt["options"][number]) => {
    if (sendingOptionId) {
      return;
    }
    setSendingOptionId(option.id);
    setSentOptionId(null);
    setFailedOptionId(null);
    setFailure(null);
    Promise.resolve()
      .then(() => onAnswer(option))
      .then(() => {
        setSentOptionId(option.id);
      })
      .catch((reason: unknown) => {
        setFailedOptionId(option.id);
        setFailure(
          reason instanceof Error && reason.message
            ? reason.message
            : "Could not send. Check the connection and try again.",
        );
      })
      .finally(() => {
        setSendingOptionId(null);
      });
  };
  return (
    <View
      style={[
        styles.card,
        {
          backgroundColor: chrome.surface,
          borderColor: chrome.borderStrong,
        },
      ]}
    >
      <View style={styles.header}>
        <View style={[styles.iconBadge, { backgroundColor: chrome.accentSoft }]}>
          <Icon name="alert-circle-fill" size={15} color={chrome.accent} />
        </View>
        <View style={styles.headerText}>
          <Text style={[styles.title, { color: chrome.text }]} numberOfLines={2}>
            {prompt.title}
          </Text>
        </View>
      </View>

      {prompt.requestText ? (
        <View
          style={[
            styles.requestBlock,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
        >
          {prompt.requestLabel ? (
            <Text style={[styles.requestLabel, { color: chrome.textMuted }]}>
              {prompt.requestLabel}
            </Text>
          ) : null}
          <ScrollView
            nestedScrollEnabled
            style={styles.requestScroll}
            showsVerticalScrollIndicator
          >
            <Text style={[styles.requestText, { color: chrome.text }]}>{prompt.requestText}</Text>
          </ScrollView>
        </View>
      ) : null}

      <View style={styles.optionColumn}>
        {prompt.options.map((option) => {
          const optionBody = (
            <>
              <View style={styles.optionCopy}>
                <View style={styles.optionTitleRow}>
                  <Text
                    style={[
                      styles.optionText,
                      {
                        color: option.primary
                          ? chrome.textOnAccent
                          : option.destructive
                            ? chrome.danger
                            : chrome.text,
                      },
                    ]}
                    numberOfLines={2}
                  >
                    {option.label}
                  </Text>
                  {option.default ? (
                    <View
                      style={[
                        styles.defaultPill,
                        {
                          backgroundColor: option.primary ? "transparent" : chrome.surface,
                          borderColor: option.primary ? chrome.textOnAccent : chrome.border,
                        },
                      ]}
                    >
                      <Text
                        style={[
                          styles.defaultText,
                          { color: option.primary ? chrome.textOnAccent : chrome.textMuted },
                        ]}
                      >
                        Default
                      </Text>
                    </View>
                  ) : null}
                </View>
                {option.description ? (
                  <Text
                    style={[
                      styles.optionDescription,
                      { color: option.primary ? chrome.textOnAccent : chrome.textMuted },
                    ]}
                    numberOfLines={2}
                  >
                    {option.description}
                  </Text>
                ) : null}
              </View>
              {sendingOptionId === option.id ? (
                <ActivityIndicator
                  size="small"
                  color={option.primary ? chrome.textOnAccent : chrome.accent}
                />
              ) : sentOptionId === option.id ? (
                <Icon
                  name="check-circle-fill"
                  size={17}
                  color={option.primary ? chrome.textOnAccent : chrome.accent}
                />
              ) : failedOptionId === option.id ? (
                <Icon name="alert-circle-fill" size={17} color={chrome.danger} />
              ) : null}
            </>
          );
          const optionStyle = [
            styles.optionButton,
            {
              backgroundColor: option.primary ? chrome.accent : chrome.surfaceMuted,
              borderColor: option.primary ? chrome.accent : chrome.border,
            },
            option.id === sentOptionId ? { borderColor: chrome.accent } : null,
            option.id === failedOptionId ? { borderColor: chrome.danger } : null,
          ];

          return (
            <Pressable
              key={option.id}
              accessibilityRole="button"
              accessibilityLabel={[
                option.label,
                option.default ? "Default action" : "",
                option.description,
              ]
                .filter(Boolean)
                .join(", ")}
              accessibilityState={{
                disabled: Boolean(sendingOptionId),
                selected: option.id === prompt.defaultOptionId,
              }}
              disabled={Boolean(sendingOptionId)}
              onPress={() => handleOptionPress(option)}
              style={({ pressed }) => [
                ...optionStyle,
                pressed ? { borderColor: chrome.focus } : null,
              ]}
            >
              {optionBody}
            </Pressable>
          );
        })}
      </View>

      {failure ? (
        <Text style={[styles.statusText, { color: chrome.danger }]} numberOfLines={3}>
          {failure}
        </Text>
      ) : null}
    </View>
  );
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
    alignItems: "flex-start",
    gap: 9,
  },
  iconBadge: {
    width: 24,
    height: 24,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 1,
  },
  headerText: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 13,
    lineHeight: 18,
    letterSpacing: 0,
  },
  requestBlock: {
    ...outlinedSurface(8),
    paddingHorizontal: 9,
    paddingVertical: 8,
    gap: 3,
  },
  requestLabel: {
    ...TypeScale.micro,
    textTransform: "uppercase",
  },
  requestScroll: {
    maxHeight: 168,
  },
  requestText: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 12,
    lineHeight: 17,
    letterSpacing: 0,
  },
  optionColumn: {
    gap: 8,
  },
  optionButton: {
    minHeight: 48,
    ...outlinedSurface(8),
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
    gap: 10,
    paddingHorizontal: 10,
    paddingVertical: 8,
  },
  optionCopy: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  optionTitleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    minWidth: 0,
  },
  optionText: {
    ...TypeScale.label,
    flexShrink: 1,
    minWidth: 0,
    fontFamily: Typography.chatFontMedium,
  },
  optionDescription: {
    ...TypeScale.caption,
  },
  defaultPill: {
    minHeight: 20,
    paddingHorizontal: 6,
    ...outlinedSurface(4),
    alignItems: "center",
    justifyContent: "center",
  },
  defaultText: {
    ...TypeScale.micro,
  },
  statusText: {
    ...TypeScale.caption,
  },
});
