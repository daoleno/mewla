import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  type SectionListRenderItem,
  SectionList,
  StyleSheet,
  Text,
  View, type ViewInstance,
} from "react-native";
import {
  ContentWidth,
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
import { Timing } from "../../constants/motion";
import { FadeInView } from "../ui/FadeInView";
import { AnimatedChevron } from "../ui/AnimatedChevron";
import { StatusMark } from "../ui/StatusMark";
import {
  buildSessionListSections,
  collapsedSectionsToStore,
  newRowKeys,
  toggleCollapsedSection,
  type SessionListSection,
} from "./sessionSections";
import {
  getCollapsedSessionSections,
  setCollapsedSessionSections,
} from "../../services/storage";
import { useWebTooltip } from "../ui/useWebTooltip";
import { useDesktopWeb } from "../navigation/useDesktopWeb";

/** The Sessions column, shared by the list, its empty states and the button. */
export const SESSIONS_COLUMN_MAX_WIDTH = ContentWidth.page;
/** Where row text and section captions start inside the column. */
export const SESSIONS_TEXT_INSET = 22;

/** The column's place: centered under the page header, whatever the width. */
export const sessionsColumnStyle = {
  width: "100%" as const,
  maxWidth: SESSIONS_COLUMN_MAX_WIDTH,
  alignSelf: "center" as const,
};
const NEW_SESSION_BUTTON_HEIGHT = 48;

const workerKeyExtractor = (agent: Worker) => agent.key;
const NO_COLLAPSED_SECTIONS: ReadonlySet<string> = new Set();

/** Keys new to the list since it last changed; see `newRowKeys`. */
function useFreshRowKeys(sections: readonly WorkerDirectorySection[]): ReadonlySet<string> {
  const knownRef = useRef<ReadonlySet<string> | null>(null);
  const fresh = useMemo(() => newRowKeys(knownRef.current, sections), [sections]);
  useEffect(() => {
    knownRef.current = new Set(
      sections.flatMap((section) => section.data.map((worker) => worker.key)),
    );
  }, [sections]);
  return fresh;
}

/** Marks the directory being folded or opened for the length of its fade. */
function useSectionMotion(toggle: (key: string) => void) {
  const [motionSectionKey, setMotionSectionKey] = useState<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    },
    [],
  );
  const toggleSection = useCallback(
    (key: string) => {
      setMotionSectionKey(key);
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setMotionSectionKey(null), Timing.base * 2);
      toggle(key);
    },
    [toggle],
  );
  return { motionSectionKey, toggleSection };
}

/**
 * The directories the person folded. Keys name their server, so another
 * server's folds never touch this list; nothing folds until the saved set is read.
 */
function useCollapsedSections() {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(NO_COLLAPSED_SECTIONS);
  const loadedRef = useRef(false);
  useEffect(() => {
    let cancelled = false;
    void getCollapsedSessionSections().then((keys) => {
      if (cancelled) return;
      loadedRef.current = true;
      setCollapsed((current) => (current.size > 0 ? new Set([...keys, ...current]) : new Set(keys)));
    });
    return () => {
      cancelled = true;
    };
  }, []);
  const toggle = useCallback((key: string) => {
    setCollapsed((current) => {
      const next = toggleCollapsedSection(current, key);
      void setCollapsedSessionSections(collapsedSectionsToStore(next));
      return next;
    });
  }, []);
  return { collapsed, toggle };
}

export interface SessionsListRowState {
  alias?: string;
  linkedWorkTitle?: string;
  calendarLabel?: string;
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
  const desktop = useDesktopWeb();
  const styles = useMemo(() => createStyles(theme, desktop), [theme, desktop]);
  const useSectionHeaders = sections.length > 1;
  const { collapsed, toggle } = useCollapsedSections();
  const { motionSectionKey, toggleSection } = useSectionMotion(toggle);
  const freshKeys = useFreshRowKeys(sections);
  const listSections = useMemo(
    () => buildSessionListSections(sections, collapsed, selectionMode),
    [collapsed, sections, selectionMode],
  );

  const renderItem = useCallback<SectionListRenderItem<Worker, SessionListSection>>(
    ({ item, section }) => {
      const state = rowState(item);
      // Rows fade in when they are new to the list or their directory was just
      // opened; rows that scroll into the window never animate.
      const enters = freshKeys.has(item.key) || section.key === motionSectionKey;
      return (
        <FadeInView style={styles.rowWrap} animate={enters}>
          <WorkerListRowContainer
            agent={item}
            alias={state.alias}
            linkedWorkTitle={state.linkedWorkTitle}
            calendarLabel={state.calendarLabel}
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
        </FadeInView>
      );
    },
    [
      freshKeys,
      motionSectionKey,
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
    ({ section }: { section: SessionListSection }) => {
      if (!useSectionHeaders) {
        return null;
      }
      const expandable = !selectionMode;
      return (
        <AnimatedPressable
          style={styles.sectionHeader}
          preset="card"
          scale={1}
          disabled={!expandable}
          onPress={() => toggleSection(section.key)}
          accessibilityRole="button"
          accessibilityLabel={`${section.title}, ${section.count} ${section.count === 1 ? "session" : "sessions"}`}
          accessibilityState={{ expanded: !section.collapsed }}
        >
          <Text
            style={styles.sectionTitle}
            numberOfLines={1}
            ellipsizeMode="middle"
          >
            {section.title}
          </Text>
          {expandable ? (
            <AnimatedChevron
              expanded={!section.collapsed}
              size={12}
              color={styles.sectionChevron.color}
            />
          ) : null}
          {section.collapsed ? (
            <>
              <Text style={styles.sectionCount}>{section.count}</Text>
              <View style={styles.sectionMark}>
                {section.mark ? <StatusMark status={section.mark} size={14} /> : null}
              </View>
            </>
          ) : null}
        </AnimatedPressable>
      );
    },
    [selectionMode, styles, toggleSection, useSectionHeaders],
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
      sections={listSections}
      ListHeaderComponent={header ?? undefined}
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
 * The page's one ink action: a round plus resting at the bottom right of the
 * Sessions column so it never competes with the rows. Its name is in the
 * label and the tooltip (with the N shortcut on desktop web), not on it.
 */
export function NewSessionButton({
  bottomInset,
  disabled,
  busy,
  onPress,
}: NewSessionButtonProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const desktop = useDesktopWeb();
  const styles = useMemo(() => createStyles(theme, desktop), [theme, desktop]);
  const inactive = disabled || busy;
  const ink = inactive ? colors.disabledText : colors.textOnAccent;
  const buttonRef = useRef<ViewInstance>(null);
  useWebTooltip(buttonRef, desktop ? "New session (N)" : "New session");
  return (
    <View
      pointerEvents="box-none"
      style={[styles.buttonDock, { bottom: bottomInset + NEW_SESSION_BUTTON_GAP }]}
    >
      <AnimatedPressable
        ref={buttonRef}
        style={[styles.button, inactive && styles.buttonDisabled]}
        preset="press"
        scale={0.97}
        onPress={onPress}
        disabled={inactive}
        accessibilityLabel="New session"
        accessibilityRole="button"
        accessibilityState={{ disabled: inactive, busy }}
      >
        {busy ? (
          <ActivityIndicator size="small" color={ink} />
        ) : (
          <Icon name="add" size={22} color={ink} />
        )}
      </AnimatedPressable>
    </View>
  );
}

function createStyles(theme: ResolvedTheme, desktop: boolean) {
  const colors = theme.colors;
  const { sectionLabel } = surfacesFromTheme(theme);
  return StyleSheet.create({
    content: {
      ...sessionsColumnStyle,
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
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      minHeight: 40,
      paddingTop: 14,
      paddingBottom: 4,
      paddingHorizontal: SESSIONS_TEXT_INSET,
    },
    sectionTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      flexShrink: 1,
      color: sectionLabel,
    },
    sectionChevron: {
      color: sectionLabel,
    },
    sectionCount: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: sectionLabel,
    },
    sectionMark: {
      marginLeft: "auto",
      width: 14,
      height: 14,
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
      width: NEW_SESSION_BUTTON_HEIGHT,
      height: NEW_SESSION_BUTTON_HEIGHT,
      borderRadius: Radii.pill,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
      ...shadow("float"),
    },
    buttonDisabled: {
      backgroundColor: colors.disabledSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
  });
}
