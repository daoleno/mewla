import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { StyleSheet, useWindowDimensions, View } from "react-native";
import BottomSheet, { BottomSheetScrollView } from "@expo/ui/community/bottom-sheet";
import { HeaderBackButton } from "../navigation/HeaderBackButton";
import { useAppColors } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { IconButton } from "../ui/IconButton";
import { InlineNotice } from "../ui/InlineNotice";
import { ListRow, ListSection } from "../ui/ListSection";
import { Spinner } from "../ui/Spinner";
import type { ProviderError, ThreadRuntimeChoice } from "../../services/providers";
import {
  reasoningEffortLabel,
  runtimeChoiceForRow,
  type ProviderPickerModelRow,
} from "../../services/providers/sessionModelHelpers";
import {
  effectRowsForRuntime,
  resolveModelSheetListMaxHeight,
} from "./sessionModelSheetModel";

interface SessionModelSheetProps {
  visible: boolean;
  loading: boolean;
  activating: boolean;
  error?: ProviderError | string | null;
  rows: ProviderPickerModelRow[];
  onRetry(): void;
  onClose(): void;
  onActivate(runtime: ThreadRuntimeChoice): void;
}

export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  rows,
  onRetry,
  onClose,
  onActivate,
}: SessionModelSheetProps) {
  const sheetRef = useRef<BottomSheet>(null);
  const [effectTarget, setEffectTarget] = useState<ProviderPickerModelRow | null>(null);
  const colors = useAppColors();
  const { height: windowHeight } = useWindowDimensions();
  const rowCount = effectTarget ? effectTarget.effects.length : rows.length;
  const listMaxHeight = resolveModelSheetListMaxHeight({ windowHeight, rowCount });
  const errorMessage = typeof error === "string" ? error : error?.message ?? null;

  useEffect(() => {
    if (visible) sheetRef.current?.present();
    else sheetRef.current?.dismiss();
  }, [visible]);
  useEffect(() => {
    if (!visible) setEffectTarget(null);
  }, [visible]);

  const activateRow = useCallback(
    (row: ProviderPickerModelRow) => {
      if (row.disabled) return;
      if (row.effects.length > 0) {
        setEffectTarget(row);
        return;
      }
      const runtime = runtimeChoiceForRow(row);
      if (runtime) onActivate(runtime);
    },
    [onActivate],
  );

  return (
    <BottomSheet
      ref={sheetRef}
      index={-1}
      enablePanDownToClose
      onDismiss={onClose}
    >
      <View style={styles.header}>
        {effectTarget ? (
          <HeaderBackButton
            surface="bare"
            color={colors.textPrimary}
            accessibilityLabel="Back to Model and Effect"
            onPress={() => setEffectTarget(null)}
          />
        ) : null}
        <AppText variant="title" style={styles.title}>
          {effectTarget ? "Effect" : "Model & Effect"}
        </AppText>
        <IconButton
          icon="close"
          tone="ghost"
          size={36}
          iconSize={20}
          accessibilityLabel="Close runtime selection"
          onPress={onClose}
        />
      </View>

      {loading ? (
        <View style={styles.center}>
          <Spinner />
        </View>
      ) : (
        <BottomSheetScrollView
          style={{ maxHeight: listMaxHeight }}
          contentContainerStyle={styles.body}
          showsVerticalScrollIndicator={false}
        >
          {effectTarget ? (
            <>
              <AppText variant="caption" tone="secondary" style={styles.context}>
                {effectTarget.modelId}
              </AppText>
              <ListSection>
                {effectRowsForRuntime(effectTarget).map(({ key, effect, selected }) => (
                  <ListRow
                    key={key}
                    title={reasoningEffortLabel(effect)}
                    accessory="check"
                    selected={selected}
                    disabled={activating}
                    accessibilityLabel={`Use ${reasoningEffortLabel(effect)} effect`}
                    accessibilityRole="button"
                    onPress={() => {
                      const runtime = runtimeChoiceForRow(effectTarget, effect);
                      if (runtime) onActivate(runtime);
                    }}
                  />
                ))}
              </ListSection>
            </>
          ) : rows.length > 0 ? (
            <ListSection>
              {rows.map((row) => (
                <ListRow
                  key={row.key}
                  title={row.label}
                  subtitle={row.unavailableCurrent ? "Current runtime is no longer selectable." : null}
                  accessory={row.effects.length > 0 ? "chevron" : "check"}
                  selected={row.current}
                  disabled={row.disabled}
                  accessibilityLabel={`Use ${row.modelId} model`}
                  accessibilityRole="button"
                  onPress={() => activateRow(row)}
                />
              ))}
            </ListSection>
          ) : null}

          {activating ? (
            <View style={styles.status}>
              <Spinner />
              <AppText variant="caption" tone="secondary">Switching…</AppText>
            </View>
          ) : null}
          {errorMessage ? (
            <InlineNotice
              tone="danger"
              title="Could not load models"
              detail={errorMessage}
              detailNumberOfLines={0}
              action={{ label: "Retry", onPress: onRetry }}
            />
          ) : null}
          {!activating && !errorMessage && rows.length === 0 ? (
            <AppText variant="body" tone="secondary">
              No configured runtime is available. Sync models in Settings.
            </AppText>
          ) : null}
        </BottomSheetScrollView>
      )}
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  header: { flexDirection: "row", alignItems: "center", paddingHorizontal: 16, paddingTop: 10, paddingBottom: 6, gap: 8 },
  title: { flex: 1 },
  center: { paddingVertical: 32, alignItems: "center" },
  body: { paddingHorizontal: 16, gap: 10, paddingBottom: 16 },
  context: { paddingHorizontal: 2 },
  status: { flexDirection: "row", alignItems: "center", gap: 8, paddingVertical: 6 },
});
