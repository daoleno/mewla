import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import * as Haptics from 'expo-haptics';
import type { TerminalThemeChrome } from '../../constants/terminalThemes';
import {
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
  useAppColors,
  type AppColors,
  type WorkerStatus,
} from '../../constants/tokens';
import { GlassSurface } from '../ui/GlassSurface';
import { workerStatusLabel } from '../../services/workerStatusPresentation';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import type { AgentKind } from '../../services/workerPresentation';
import type { TerminalFlavor } from '../../services/terminalFlavor';
import { AgentKindIcon } from './AgentKindIcon';
import {
  CHAT_CHROME_HORIZONTAL_INSET,
  CHAT_HEADER_HEIGHT,
  CHAT_HEADER_OUTER_GAP,
} from './chatChromeMetrics';
import { SessionAvatar } from '../ui/SessionAvatar';
import { HeaderBackButton } from '../navigation/HeaderBackButton';
import { relativeLuminance } from '../../theme/colorUtils';
import { StatusMark } from '../ui/StatusMark';
import type { WorkStatus } from '../ui/workStatus';
import { Icon, type IconName } from "../icons/Icon";

interface TelegramChatHeaderAction {
  key: string;
  icon: IconName;
  accessibilityLabel: string;
  disabled?: boolean;
  /** Optional override; defaults to muted header icon color. */
  iconColor?: string;
  /** Short coloured figures beside the icon, e.g. `+12` and `\u22123`. */
  stats?: { text: string; color?: string }[];
  /** A dot on the icon's corner. */
  markColor?: string;
  onPress: () => void;
}

interface TelegramChatHeaderProps {
  /** When set, header shares the chat canvas instead of app shell colors. */
  chrome?: TerminalThemeChrome;
  title: string;
  subtitle?: string;
  /** A glyph before the subtitle, e.g. the calendar for Calendar-born work. */
  subtitleIcon?: IconName;
  avatarLabel?: string;
  avatarSeed?: string;
  agentKind?: AgentKind;
  terminalFlavor?: TerminalFlavor;
  avatar?: React.ReactNode;
  onBack?: () => void;
  onPressTitle?: () => void;
  /** What tapping the title does, for assistive tech ("Open in Calendar"). */
  titleActionLabel?: string;
  rightActions?: TelegramChatHeaderAction[];
  menuAnchorRef?: React.RefObject<View | null>;
  flat?: boolean;
  /** Live Session state, shown as its StatusMark on the avatar. */
  status?: WorkerStatus;
}

export function TelegramChatHeader({
  chrome,
  title,
  subtitle,
  subtitleIcon,
  avatarLabel,
  avatarSeed,
  agentKind,
  terminalFlavor,
  avatar,
  onBack,
  onPressTitle,
  titleActionLabel,
  rightActions = [],
  menuAnchorRef,
  flat = false,
  status,
}: TelegramChatHeaderProps) {
  const colors = useAppColors();
  const styles = useMemo(
    () => createStyles(colors, chrome),
    [chrome, colors],
  );
  const avatarText = avatarLabel ?? title;
  const avatarKey = avatarSeed ?? title;
  const statusMark = status ? HEADER_STATUS_MARK[status] : null;
  const glass = flat ? null : styles.glass;
  // Capsules keep the chat canvas' own contrast logic; GlassSurface only
  // supplies the material, hairline and lit edge.
  const Capsule = flat ? View : GlassSurface;
  const capsuleProps = flat
    ? {}
    : {
        material: 'chrome' as const,
        radius: CHAT_HEADER_HEIGHT / 2,
        elevation: 'card' as const,
        fill: glass?.backgroundColor,
        stroke: glass?.borderColor,
      };

  return (
    <View style={[styles.outer, flat ? styles.outerFlat : null]}>
      <View style={[styles.row, flat ? styles.rowFlat : null]}>
        {onBack ? (
          <Capsule
            {...capsuleProps}
            style={[
              styles.chip,
              styles.circleChip,
              flat ? styles.flatChrome : null,
            ]}
          >
            <HeaderBackButton
              surface="bare"
              color={styles.iconColor.color}
              onPress={onBack}
            />
          </Capsule>
        ) : null}

        <Capsule
          {...capsuleProps}
          style={[
            styles.chip,
            styles.identityCapsule,
            flat ? styles.identityFlat : null,
          ]}
        >
        <AnimatedPressable
          accessibilityRole="button"
          accessibilityLabel={[
            title,
            status ? workerStatusLabel(status) : null,
            subtitle ?? null,
            onPressTitle ? titleActionLabel ?? null : null,
          ]
            .filter(Boolean)
            .join(', ')}
          disabled={!onPressTitle}
          style={styles.identityPill}
          preset="press"
          scale={0.99}
          onPress={() => {
            if (!onPressTitle) {
              return;
            }
            Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
            onPressTitle();
          }}
        >
          <View>
            {avatar ? (
              avatar
            ) : agentKind ? (
              <AgentKindIcon
                kind={agentKind}
                flavor={terminalFlavor}
                variant="avatar"
              />
            ) : (
              <SessionAvatar label={avatarText} seed={avatarKey} size={30} />
            )}
            {statusMark ? (
              <View
                pointerEvents="none"
                accessibilityElementsHidden
                importantForAccessibility="no-hide-descendants"
                style={[
                  styles.statusBadge,
                  { backgroundColor: styles.glass.backgroundColor },
                ]}
              >
                <StatusMark
                  status={statusMark}
                  size={11}
                  knockout={styles.glass.backgroundColor}
                />
              </View>
            ) : null}
          </View>
          <View style={styles.copy}>
            <Text style={styles.title} numberOfLines={1}>
              {title}
            </Text>
            {subtitle ? (
              <View style={styles.subtitleRow}>
                {subtitleIcon ? (
                  <Icon name={subtitleIcon} size={12} color={styles.subtitle.color} />
                ) : null}
                <Text style={styles.subtitle} numberOfLines={1}>
                  {subtitle}
                </Text>
              </View>
            ) : null}
          </View>
        </AnimatedPressable>
        </Capsule>

        {rightActions.length > 0 ? (
          <View
            ref={menuAnchorRef}
            collapsable={false}
            style={styles.actionsAnchor}
          >
          <Capsule
            {...capsuleProps}
            style={[
              styles.chip,
              styles.actionsChip,
              flat ? styles.actionsFlat : null,
            ]}
          >
            {rightActions.map((action) => (
              <AnimatedPressable
                key={action.key}
                accessibilityRole="button"
                accessibilityLabel={action.accessibilityLabel}
                accessibilityState={{ disabled: action.disabled }}
                disabled={action.disabled}
                style={[
                  styles.iconButton,
                  flat ? styles.iconButtonFlat : null,
                  action.stats?.length && !action.disabled
                    ? styles.iconButtonStats
                    : null,
                ]}
                preset="press"
                scale={0.9}
                onPress={() => {
                  if (action.disabled) {
                    return;
                  }
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  action.onPress();
                }}
              >
                <View>
                  <Icon
                    name={action.icon}
                    size={20}
                    color={
                      action.disabled
                        ? colors.disabledText
                        : action.iconColor ?? styles.iconMuted.color
                    }
                  />
                  {action.markColor && !action.disabled ? (
                    <View
                      pointerEvents="none"
                      style={[styles.iconMark, { backgroundColor: action.markColor }]}
                    />
                  ) : null}
                </View>
                {!action.disabled && action.stats?.length ? (
                  <Text style={styles.stats} numberOfLines={1}>
                    {action.stats.map((stat, index) => (
                      <Text
                        key={stat.text}
                        style={stat.color ? { color: stat.color } : null}
                      >
                        {index > 0 ? ' ' : ''}
                        {stat.text}
                      </Text>
                    ))}
                  </Text>
                ) : null}
              </AnimatedPressable>
            ))}
          </Capsule>
          </View>
        ) : null}
      </View>
    </View>
  );
}

// Only states that ask for a glance get a mark; a finished or idle Session
// keeps a clean avatar.
const HEADER_STATUS_MARK: Record<WorkerStatus, WorkStatus | null> = {
  running: 'running',
  blocked: 'blocked',
  failed: 'failed',
  done: null,
  unknown: null,
};

function resolveChipSurface(
  colors: AppColors,
  chrome?: TerminalThemeChrome,
): string {
  if (!chrome) {
    return colors.bgSurface;
  }
  const canvas = chrome.appBackground;
  const candidates = [chrome.composerInput, chrome.surface, colors.bgSurface];
  for (const candidate of candidates) {
    if (!candidate.startsWith('#') || !canvas.startsWith('#')) {
      continue;
    }
    if (
      Math.abs(relativeLuminance(candidate) - relativeLuminance(canvas)) >= 0.04
    ) {
      return candidate;
    }
  }
  return colors.bgSurface;
}

function createStyles(colors: AppColors, chrome?: TerminalThemeChrome) {
  const chipSurface = resolveChipSurface(colors, chrome);
  const titleColor = chrome?.text ?? colors.textPrimary;
  const subtitleColor = chrome?.textMuted ?? colors.textSecondary;
  const iconColor = chrome?.text ?? colors.textPrimary;
  const iconMuted = chrome?.textMuted ?? colors.textSecondary;
  const borderColor = chrome?.border ?? colors.borderSubtle;

  return StyleSheet.create({
    outer: {
      paddingHorizontal: CHAT_CHROME_HORIZONTAL_INSET,
      paddingTop: CHAT_HEADER_OUTER_GAP,
      paddingBottom: CHAT_HEADER_OUTER_GAP,
      backgroundColor: 'transparent',
      zIndex: 3,
    },
    outerFlat: {
      paddingTop: 0,
      paddingBottom: 0,
      // The bar behind it (hairline included) is full width, in TerminalScreenLayout.
      backgroundColor: 'transparent',
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      height: CHAT_HEADER_HEIGHT,
      gap: 8,
    },
    rowFlat: {
      height: 52,
    },
    glass: {
      backgroundColor: chipSurface,
      borderColor,
    },
    chip: {
      height: CHAT_HEADER_HEIGHT,
      flexDirection: 'row',
      alignItems: 'center',
      overflow: 'hidden',
    },
    actionsAnchor: {
      flexShrink: 0,
    },
    identityCapsule: {
      flex: 1,
      minWidth: 0,
    },
    statusBadge: {
      position: 'absolute',
      right: -3,
      bottom: -3,
      width: 15,
      height: 15,
      borderRadius: 8,
      alignItems: 'center',
      justifyContent: 'center',
    },
    circleChip: {
      width: CHAT_HEADER_HEIGHT,
      justifyContent: 'center',
    },
    flatChrome: {
      height: 52,
      backgroundColor: 'transparent',
      borderWidth: 0,
      borderRadius: 0,
    },
    identityPill: {
      flex: 1,
      minWidth: 0,
      alignSelf: 'stretch',
      flexDirection: 'row',
      alignItems: 'center',
      gap: 10,
      paddingLeft: 4,
      paddingRight: 14,
      opacity: 1,
    },
    identityFlat: {
      height: 52,
      paddingLeft: 2,
      paddingRight: 8,
      backgroundColor: 'transparent',
      borderWidth: 0,
      borderRadius: 0,
    },
    actionsChip: {
      flexShrink: 0,
      paddingHorizontal: 2,
    },
    actionsFlat: {
      height: 52,
      paddingHorizontal: 0,
      backgroundColor: 'transparent',
      borderWidth: 0,
      borderRadius: 0,
    },
    iconButton: {
      minWidth: 44,
      height: 44,
      borderRadius: Radii.pill,
      alignItems: 'center',
      justifyContent: 'center',
      opacity: 1,
    },
    iconButtonFlat: {
      minWidth: 44,
      height: 44,
    },
    iconButtonStats: {
      flexDirection: 'row',
      gap: 4,
      paddingHorizontal: 8,
    },
    iconMark: {
      position: 'absolute',
      top: -1,
      right: -2,
      width: 7,
      height: 7,
      borderRadius: 4,
    },
    stats: {
      ...UiTextMetrics,
      ...TypeScale.micro,
      color: iconMuted,
      fontFamily: Typography.uiFontMedium,
      fontVariant: ['tabular-nums'],
    },
    iconColor: {
      color: iconColor,
    },
    iconMuted: {
      color: iconMuted,
    },
    copy: {
      flex: 1,
      minWidth: 0,
      justifyContent: 'center',
      gap: 0,
    },
    title: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: titleColor,
      fontFamily: Typography.uiFontMedium,
    },
    subtitleRow: { flexDirection: 'row', alignItems: 'center', gap: 4, minWidth: 0 },
    subtitle: {
      flexShrink: 1,
      ...UiTextMetrics,
      ...TypeScale.micro,
      color: subtitleColor,
      fontFamily: Typography.uiFont,
      fontWeight: '400',
    },
  });
}
