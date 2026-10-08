import React, { useMemo, useState } from 'react';
import {
  Platform,
  StyleSheet,
  Text,
  View,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import {
  TypeScale,
  UiTextMetrics,
  type WorkerStatus,
  useAppColors,
  type AppColors,
} from '../../constants/tokens';
import type { AgentKind } from '../../services/workerPresentation';
import type { TerminalFlavor } from '../../services/terminalFlavor';
import type { SessionPreviewTone } from '../../services/sessionPreview';
import {
  buildWorkerSessionAccessibilityLabel,
  isWorkerActivelyRunning,
} from '../../services/workerStatusPresentation';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import { useDesktopWeb } from '../navigation/useDesktopWeb';
import { AgentKindIcon } from '../terminal/AgentKindIcon';
import { StatusMark } from '../ui/StatusMark';
import type { WorkStatus } from '../ui/workStatus';
import { Icon } from "../icons/Icon";

interface WorkerSessionRowProps {
  title: string;
  kind: AgentKind;
  terminalFlavor?: TerminalFlavor;
  preview: string;
  previewTone: SessionPreviewTone;
  previewPrefix?: string;
  timeLabel: string;
  status: WorkerStatus;
  /** The Session waits on you: the seal mark replaces the status glyph. */
  needsYou?: boolean;
  brainDelegated?: boolean;
  onPress: () => void;
  onLongPress: () => void;
  /** Selection mode: taps toggle selection instead of opening the Session. */
  selectionMode?: boolean;
  selected?: boolean;
  /** Row cannot be terminated (e.g. daemon offline): disabled inside selection. */
  selectionDisabled?: boolean;
  onToggleSelection?: () => void;
  separator?: boolean;
  /** Corner radii of the grouped card position, so the row's own active
   * background is rounded without an ancestor clip. */
  cornerStyle?: StyleProp<ViewStyle>;
}

export function WorkerSessionRow({
  title,
  kind,
  terminalFlavor,
  preview,
  previewTone,
  previewPrefix,
  timeLabel,
  status,
  needsYou = false,
  brainDelegated = false,
  onPress,
  onLongPress,
  selectionMode = false,
  selected = false,
  selectionDisabled = false,
  onToggleSelection,
  separator = true,
  cornerStyle,
}: WorkerSessionRowProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const previewColor = previewToneColor(previewTone, colors);
  const activelyRunning = isWorkerActivelyRunning(status);
  const statusMark = needsYou ? 'needs' : WORKER_STATUS_MARK[status];
  const inSelectionMode = selectionMode;
  const rowDisabled = inSelectionMode && selectionDisabled;
  // Web: a mouse shows where a click lands, and a right-click selects the
  // row the way a long press does on the phone.
  // Desktop only: Android Chrome fires contextmenu on a long press too.
  const [hovered, setHovered] = useState(false);
  const desktopWeb = useDesktopWeb();
  const webPointerProps =
    Platform.OS === 'web'
      ? {
          onHoverIn: () => setHovered(true),
          onHoverOut: () => setHovered(false),
          ...(desktopWeb
            ? {
                onContextMenu: (event: { preventDefault(): void }) => {
                  if (inSelectionMode) return;
                  event.preventDefault();
                  onLongPress();
                },
              }
            : null),
        }
      : {};

  return (
    <AnimatedPressable
      {...webPointerProps}
      style={[
        styles.row,
        cornerStyle,
        hovered && !(inSelectionMode && selected) && styles.rowHover,
        inSelectionMode && selected && styles.rowActive,
      ]}
      preset="card"
      onPress={inSelectionMode ? onToggleSelection : onPress}
      onLongPress={onLongPress}
      delayLongPress={400}
      disabled={rowDisabled}
      accessibilityRole={inSelectionMode ? 'checkbox' : 'button'}
      accessibilityLabel={buildWorkerSessionAccessibilityLabel({
        title,
        status,
        preview,
        timeLabel,
        brainDelegated,
      })}
      accessibilityHint={
        inSelectionMode
          ? 'Double tap to toggle selection'
          : 'Opens the terminal session'
      }
      accessibilityState={{
        busy: activelyRunning,
        checked: inSelectionMode ? selected : undefined,
        disabled: rowDisabled,
      }}
    >
      {inSelectionMode ? (
        <View
          style={styles.selectionSlot}
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
        >
          <Icon
            name={selected ? 'check-circle-fill' : 'circle'}
            size={24}
            color={selected ? colors.accent : colors.borderStrong}
          />
        </View>
      ) : null}
      <View style={styles.iconSlot}>
        <AgentKindIcon kind={kind} flavor={terminalFlavor} size={36} />
        {brainDelegated ? (
          <View
            pointerEvents="none"
            style={styles.brainOriginMarker}
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
          >
            <Icon name="git-network" size={9} color={colors.accentStrong} />
          </View>
        ) : null}
      </View>
      <View style={styles.body}>
        <Text style={styles.title} numberOfLines={1}>
          {title}
        </Text>
        <Text style={styles.preview} numberOfLines={1} ellipsizeMode="middle">
          {previewPrefix ? (
            <Text style={[styles.previewPrefix, { color: previewColor }]}>
              {previewPrefix}
            </Text>
          ) : null}
          <Text style={[styles.previewText, { color: previewColor }]}>
            {preview}
          </Text>
        </Text>
      </View>
      <View style={styles.meta}>
        {!activelyRunning ? (
          <Text style={styles.time} numberOfLines={1}>
            {timeLabel}
          </Text>
        ) : null}
        <View
          pointerEvents="none"
          style={styles.statusIndicator}
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
        >
          {statusMark ? <StatusMark status={statusMark} size={14} /> : null}
        </View>
      </View>
      {separator ? <View pointerEvents="none" style={styles.separator} /> : null}
    </AnimatedPressable>
  );
}

function previewToneColor(tone: SessionPreviewTone, colors: AppColors): string {
  switch (tone) {
    case 'accent':
      return colors.accent;
    case 'danger':
      return colors.dangerText;
    case 'needs':
      return colors.sealText;
    case 'success':
      return colors.success;
    case 'muted':
      return colors.textTertiary;
    default:
      return colors.textSecondary;
  }
}

// Seal & Slip glyphs; an idle session carries no mark.
const WORKER_STATUS_MARK: Record<WorkerStatus, WorkStatus | null> = {
  running: 'running',
  done: 'ready',
  failed: 'failed',
  blocked: 'blocked',
  unknown: null,
};

function createStyles(colors: AppColors) {
  return StyleSheet.create({
    iconSlot: {
      width: 44,
      height: 44,
      alignItems: 'center',
      justifyContent: 'center',
    },
    // Selection takes its own leading column so the check never covers the
    // agent's mark.
    selectionSlot: {
      width: 24,
      height: 24,
      marginRight: -2,
      alignItems: 'center',
      justifyContent: 'center',
    },
    brainOriginMarker: {
      position: 'absolute',
      right: 0,
      bottom: 0,
      width: 16,
      height: 16,
      borderRadius: 8,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderStrong,
      zIndex: 2,
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      minHeight: 68,
      gap: 12,
      paddingVertical: 10,
      paddingHorizontal: 14,
      backgroundColor: 'transparent',
    },
    rowHover: {
      backgroundColor: colors.surfaceSubtle,
    },
    rowActive: {
      backgroundColor: colors.accentSoft,
    },
    separator: {
      position: 'absolute',
      left: 70,
      right: 0,
      bottom: 0,
      height: StyleSheet.hairlineWidth,
      backgroundColor: colors.borderSubtle,
    },
    body: {
      flex: 1,
      minWidth: 0,
      justifyContent: 'center',
    },
    title: {
      ...UiTextMetrics,
      ...TypeScale.body,
      fontFamily: TypeScale.label.fontFamily,
      color: colors.textPrimary,
    },
    preview: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 1,
    },
    previewPrefix: {
      fontFamily: TypeScale.compact.fontFamily,
    },
    previewText: {
      fontFamily: TypeScale.compact.fontFamily,
    },
    meta: {
      alignItems: 'flex-end',
      gap: 4,
    },
    time: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    statusIndicator: {
      width: 16,
      height: 16,
      alignItems: 'center',
      justifyContent: 'center',
    },
  });
}
