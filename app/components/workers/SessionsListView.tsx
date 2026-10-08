import React, { useCallback, useMemo } from "react";
import {
  type ListRenderItem,
  SectionList,
  StyleSheet,
  Text,
  View,
} from "react-native";
import {
  ContinuousCorners,
  Radii,
  TypeScale,
  UiTextMetrics,
  shadow,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedTheme } from "../../theme";
import { surfacesFromTheme } from "../../constants/themedSurfaces";
import type { Worker } from "../../store/workers";
import type { WorkerDirectorySection } from "../../services/workerDirectory";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { WorkerListRowContainer } from "./WorkerListRowContainer";
import { Icon } from "../icons/Icon";

/** The Sessions column, shared by the list, its empty states and the button. */
export const SESSIONS_COLUMN_MAX_WIDTH = 760;
const NEW_SESSION_BUTTON_HEIGHT = 40;

const workerKeyExtractor = (agent: Worker) => agent.key;

export interface SessionsListRowState {
  alias?: string;
  linkedWorkTitle?: string;
  selected: boolean;
  selectionDisabled: boolean;
}

interface SessionsListViewProps {
  sections: WorkerDirectorySection[];
  header?: React.ReactElement | null;
  rowState(agent: Worker): SessionsListRowState;
  selectionMode: boolean;
  showServerName: boolean;
  bottomInset: number;
  onOpenWorker(agent: Worker): void;
  onEnterSelection(agent: Worker): void;
  onToggleSelection(agent: Worker): void;
}

/**
 * Seal & Slip Sessions: plain rows on the paper, grouped by directory with a
 * quiet caption when there is more than one directory. No cards, no
 * separators; the press and selection tint is the only fill.
 */
export function SessionsListView({
  sections,
  header,
  rowState,
  selectionMode,
  showServerName,
  bottomInset,
  onOpenWorker,
  onEnterSelection,
  onToggleSelection,
}: SessionsListViewProps) {
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const useSectionHeaders = sections.length > 1;

  const renderItem = useCallback<ListRenderItem<Worker>>(
    ({ item }) => {
      const state = rowState(item);
      return (
        <View style={styles.rowWrap}>
          <WorkerListRowContainer
            agent={item}
            alias={state.alias}
            linkedWorkTitle={state.linkedWorkTitle}
            showServerName={showServerName}
            selectionMode={selectionMode}
            selected={state.selected}
            selectionDisabled={state.selectionDisabled}
            onOpenWorker={onOpenWorker}
            onEnterSelection={onEnterSelection}
            onToggleSelection={onToggleSelection}
            separator={false}
            cornerStyle={styles.rowCorners}
          />
        </View>
      );
    },
    [
      onEnterSelection,
      onOpenWorker,
      onToggleSelection,
      rowState,
      selectionMode,
      showServerName,
      styles,
    ],
  );

  const renderSectionHeader = useCallback(
    ({ section }: { section: WorkerDirectorySection }) => {
      if (!useSectionHeaders) {
        return null;
      }
      return (
        <View style={styles.sectionHeader}>
          <Text
            style={styles.sectionTitle}
            numberOfLines={1}
            ellipsizeMode="middle"
          >
            {section.title}
          </Text>
        </View>
      );
    },
    [styles, useSectionHeaders],
  );

  const contentContainerStyle = useMemo(
    () => [
      styles.content,
      {
        paddingBottom:
          bottomInset + NEW_SESSION_BUTTON_HEIGHT + NEW_SESSION_BUTTON_GAP * 2,
      },
    ],
    [bottomInset, styles],
  );

  return (
    <SectionList
      sections={sections}
      ListHeaderComponent={header}
      key="list"
      keyExtractor={workerKeyExtractor}
      renderItem={renderItem}
      renderSectionHeader={renderSectionHeader}
      stickySectionHeadersEnabled={false}
      contentContainerStyle={contentContainerStyle}
      alwaysBounceVertical
      removeClippedSubviews={false}
      windowSize={15}
      showsVerticalScrollIndicator={false}
    />
  );
}

const NEW_SESSION_BUTTON_GAP = 12;

interface NewSessionButtonProps {
  bottomInset: number;
  disabled: boolean;
  busy: boolean;
  onPress(): void;
}

/**
 * The page's one ink pill: compact, content-width, resting at the bottom
 * right of the Sessions column so it never competes with the rows.
 */
export function NewSessionButton({
  bottomInset,
  disabled,
  busy,
  onPress,
}: NewSessionButtonProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const styles = useMemo(() => createStyles(theme), [theme]);
  const inactive = disabled || busy;
  const ink = inactive ? colors.disabledText : colors.textOnAccent;
  return (
    <View
      pointerEvents="box-none"
      style={[styles.buttonDock, { bottom: bottomInset + NEW_SESSION_BUTTON_GAP }]}
    >
      <AnimatedPressable
        style={[styles.button, inactive && styles.buttonDisabled]}
        preset="press"
        scale={0.97}
        onPress={onPress}
        disabled={inactive}
        accessibilityLabel="New session"
        accessibilityRole="button"
        accessibilityState={{ disabled: inactive, busy }}
      >
        <Icon name="add" size={18} color={ink} />
        <Text style={[styles.buttonLabel, { color: ink }]} numberOfLines={1}>
          {busy ? "Starting…" : "New session"}
        </Text>
      </AnimatedPressable>
    </View>
  );
}

function createStyles(theme: ResolvedTheme) {
  const colors = theme.colors;
  const { sectionLabel } = surfacesFromTheme(theme);
  return StyleSheet.create({
    content: {
      width: "100%",
      maxWidth: SESSIONS_COLUMN_MAX_WIDTH,
      alignSelf: "center",
      paddingTop: 4,
    },
    rowWrap: {
      marginHorizontal: 8,
    },
    rowCorners: {
      borderRadius: Radii.md,
      ...ContinuousCorners,
    },
    sectionHeader: {
      paddingTop: 18,
      paddingBottom: 4,
      paddingHorizontal: 22,
    },
    sectionTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: sectionLabel,
    },
    buttonDock: {
      position: "absolute",
      left: 0,
      right: 0,
      alignItems: "flex-end",
      paddingHorizontal: 16,
      zIndex: 4,
    },
    button: {
      height: NEW_SESSION_BUTTON_HEIGHT,
      borderRadius: Radii.pill,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 4,
      paddingLeft: 12,
      paddingRight: 16,
      backgroundColor: colors.accent,
      ...shadow("float"),
    },
    buttonDisabled: {
      backgroundColor: colors.disabledSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    buttonLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
    },
  });
}
