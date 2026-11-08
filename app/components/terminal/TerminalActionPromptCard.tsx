import React, { useEffect, useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { Typography, useAppColors } from "../../constants/tokens";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";
import { AppText } from "../ui/AppText";
import { InlineNotice } from "../ui/InlineNotice";
import { ListRow, ListSection } from "../ui/ListSection";
import { StatusPill } from "../ui/StatusPill";
import { Icon } from "../icons/Icon";

interface TerminalActionPromptCardProps {
  prompt: TerminalActionPrompt;
  /** Answers with the chosen option: its menu number, or the agent's own approval answer. */
  onAnswer(option: TerminalActionPrompt["options"][number]): Promise<void> | void;
}

/** Reads the terminal theme from the surrounding `TerminalThemeScope`. */
export function TerminalActionPromptCard({
  prompt,
  onAnswer,
}: TerminalActionPromptCardProps) {
  const colors = useAppColors();
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
    <View style={styles.card}>
      <AppText variant="label" numberOfLines={2} style={styles.title}>
        {prompt.title}
      </AppText>

      {prompt.requestText ? (
        <View style={[styles.requestBlock, { backgroundColor: colors.surfaceSubtle, borderColor: colors.border }]}>
          {prompt.requestLabel ? (
            <AppText variant="micro" tone="secondary" style={styles.requestLabel}>
              {prompt.requestLabel}
            </AppText>
          ) : null}
          <ScrollView nestedScrollEnabled style={styles.requestScroll} showsVerticalScrollIndicator>
            <AppText variant="caption" style={styles.requestText}>{prompt.requestText}</AppText>
          </ScrollView>
        </View>
      ) : null}

      <ListSection>
        {prompt.options.map((option) => (
          <ListRow
            key={option.id}
            title={option.label}
            subtitle={option.description || null}
            numberOfLines={2}
            subtitleNumberOfLines={2}
            destructive={option.destructive}
            disabled={Boolean(sendingOptionId)}
            loading={sendingOptionId === option.id}
            accessibilityLabel={[option.label, option.default ? "Default action" : "", option.description]
              .filter(Boolean)
              .join(", ")}
            trailing={
              option.default || sentOptionId === option.id || failedOptionId === option.id ? (
                <View style={styles.trailing}>
                  {option.default ? <StatusPill label="Default" /> : null}
                  {sentOptionId === option.id ? <Icon name="check-circle-fill" size={17} color={colors.accent} /> : null}
                  {failedOptionId === option.id ? <Icon name="alert-circle-fill" size={17} color={colors.dangerText} /> : null}
                </View>
              ) : undefined
            }
            onPress={() => handleOptionPress(option)}
          />
        ))}
      </ListSection>

      {failure ? <InlineNotice tone="danger" title="Could not send" detail={failure} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: { marginHorizontal: 12, marginBottom: 8, gap: 10 },
  title: { paddingHorizontal: 4 },
  requestBlock: { borderRadius: 8, borderWidth: StyleSheet.hairlineWidth, paddingHorizontal: 9, paddingVertical: 8, gap: 3 },
  requestLabel: { textTransform: "uppercase" },
  requestScroll: { maxHeight: 168 },
  requestText: { fontFamily: Typography.chatMonoFont },
  trailing: { flexDirection: "row", alignItems: "center", gap: 6 },
});
