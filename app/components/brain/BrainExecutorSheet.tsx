import { useMemo } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from "react-native";
import * as Haptics from "expo-haptics";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { outlinedSurface } from "../ui/outlinedSurface";
import {
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { surfacesFromTheme } from "../../constants/themedSurfaces";
import type { BrainExecutorRef } from "../../store/brain";
import { BrainExecutorIcon } from "./BrainExecutorIcon";
import { brainAdapterLabel, brainProviderLabel } from "./brainPresentation";
import { Icon } from "../icons/Icon";

interface BrainExecutorSheetProps {
  visible: boolean;
  executors: BrainExecutorRef[];
  hostAdapterId?: string;
  switchingAdapterId: string | null;
  error?: string | null;
  onClose: () => void;
  onSelect: (adapter: BrainExecutorRef) => void;
}

export function BrainExecutorSheet({
  visible,
  executors,
  hostAdapterId,
  switchingAdapterId,
  error,
  onClose,
  onSelect,
}: BrainExecutorSheetProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const themed = useMemo(() => surfacesFromTheme(theme), [theme]);
  const styles = useMemo(() => createStyles(theme), [theme]);
  const interactionLocked = Boolean(switchingAdapterId);

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      keyboardAvoiding
      maxHeight="72%"
      contentStyle={styles.sheetContent}
    >
      <Text style={styles.title}>Brain host</Text>
      <Text style={styles.lead}>
        Brain picks each Worker's executor from its routing guide. Running
        sessions keep their current executor.
      </Text>

      <ScrollView contentContainerStyle={styles.list} keyboardShouldPersistTaps="handled">
        {executors.map((adapter) => {
          const disabled = interactionLocked;
          const active = adapter.id === hostAdapterId;
          const rowShowsSpinner =
            interactionLocked && switchingAdapterId === adapter.id;
          const provider = brainProviderLabel(adapter.provider);
          const label = brainAdapterLabel(adapter);
          const titleColor = interactionLocked
            ? colors.disabledText
            : colors.textPrimary;
          const metaColor = interactionLocked
            ? colors.disabledText
            : colors.textTertiary;

          return (
            <AnimatedPressable
              key={adapter.id}
              accessibilityRole="button"
              accessibilityState={{
                disabled,
                busy: rowShowsSpinner,
              }}
              accessibilityLabel={`Set Brain host to ${label}`}
              disabled={disabled}
              preset="press"
              scale={0.98}
              style={[
                styles.row,
                {
                  borderColor: active ? colors.accent : themed.border,
                  backgroundColor: disabled
                    ? colors.disabledSurface
                    : active
                      ? colors.surfaceActive
                      : themed.surface,
                },
              ]}
              onPress={() => {
                if (disabled) {
                  return;
                }
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                onSelect(adapter);
              }}
            >
              <BrainExecutorIcon adapter={adapter} size={17} />
              <View style={styles.rowMain}>
                <Text
                  style={[styles.rowTitle, { color: titleColor }]}
                  numberOfLines={1}
                >
                  {label}
                </Text>
                <Text
                  style={[styles.rowMeta, { color: metaColor }]}
                >
                  {provider}
                  {adapter.runtime?.trim()
                    ? ` · ${adapter.runtime.trim()}`
                    : ""}
                </Text>
              </View>
              {rowShowsSpinner ? (
                <ActivityIndicator size="small" color={colors.accent} />
              ) : active ? (
                <Icon
                  name="check-circle-fill"
                  size={20}
                  color={
                    interactionLocked ? colors.disabledText : colors.accent
                  }
                />
              ) : (
                <Icon
                  name="circle"
                  size={18}
                  color={
                    interactionLocked
                      ? colors.disabledText
                      : colors.textTertiary
                  }
                />
              )}
            </AnimatedPressable>
          );
        })}
      </ScrollView>

      {error ? <Text style={styles.error}>{error}</Text> : null}
    </BottomSheetFrame>
  );
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  return StyleSheet.create({
    sheetContent: {
      flexShrink: 1,
      minHeight: 0,
    },
    title: {
      ...UiTextMetrics,
      ...TypeScale.title,
      color: colors.textPrimary,
      marginBottom: 6,
    },
    lead: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textSecondary,
      marginBottom: 14,
    },
    list: {
      gap: 10,
    },
    row: {
      minHeight: 64,
      ...outlinedSurface(Radii.md),
      paddingHorizontal: 14,
      paddingVertical: 12,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
    },
    rowMain: {
      flex: 1,
      minWidth: 0,
      gap: 2,
    },
    rowTitle: {
      ...UiTextMetrics,
      fontFamily: Typography.uiFontMedium,
      fontSize: 15,
      lineHeight: uiLineHeight(15),
    },
    rowMeta: {
      ...UiTextMetrics,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: uiLineHeight(12),
    },
    error: {
      ...TypeScale.caption,
      marginTop: 12,
      color: colors.dangerText,
    },
  });
}
