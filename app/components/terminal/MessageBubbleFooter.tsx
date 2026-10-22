import React from 'react';
import { Alert, Pressable, StyleSheet, Text, View } from 'react-native';
import { formatChatBubbleTime } from '../../constants/telegramPresentation';
import { TouchTarget, Typography, useAppTheme } from '../../constants/tokens';
import { PENDING_MESSAGE_UNDELIVERED_ACCESSIBILITY_LABEL } from './pendingUserMessageLifecycle';

/** 20 pt mark + hitSlop = platform touch target (44 pt / 48 dp). */
const UNDELIVERED_MARK_SIZE = 20;
const UNDELIVERED_HIT_SLOP = (TouchTarget - UNDELIVERED_MARK_SIZE) / 2;

interface MessageBubbleFooterProps {
  timestamp?: string;
  tone?: 'sent' | 'received';
  undeliveredColor?: string;
  undeliveredLabel?: string;
  /** Present only for a message that could not be delivered. */
  onResend?: () => void;
  onDelete?: () => void;
}

export function MessageBubbleFooter({
  timestamp,
  tone = 'received',
  undeliveredColor,
  undeliveredLabel = PENDING_MESSAGE_UNDELIVERED_ACCESSIBILITY_LABEL,
  onResend,
  onDelete,
}: MessageBubbleFooterProps) {
  const { theme } = useAppTheme();
  // Timestamps stay available so enabling them does not shift geometry when
  // the optimistic row becomes durable.
  const label = formatChatBubbleTime(timestamp);
  const timeColor =
    tone === 'sent'
      ? theme.chat.sentTimestamp
      : theme.chat.receivedTimestamp;
  if (!label && !onResend) {
    return null;
  }
  const markColor = undeliveredColor || timeColor;

  const confirmDelete = () => {
    if (!onDelete) return;
    Alert.alert('Delete this message?', undefined, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Delete', style: 'destructive', onPress: onDelete },
    ]);
  };

  return (
    <View style={styles.row}>
      {label ? (
        <Text style={[styles.time, { color: timeColor }]}>{label}</Text>
      ) : null}
      {onResend ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={undeliveredLabel}
          hitSlop={UNDELIVERED_HIT_SLOP}
          onPress={onResend}
          onLongPress={onDelete ? confirmDelete : undefined}
          style={({ pressed }) => [
            styles.mark,
            { borderColor: markColor, opacity: pressed ? 0.56 : 1 },
          ]}
        >
          <Text style={[styles.markGlyph, { color: markColor }]}>!</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
    alignSelf: 'flex-end',
    gap: 6,
    marginTop: 2,
    paddingTop: 2,
  },
  time: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
  },
  mark: {
    width: UNDELIVERED_MARK_SIZE,
    height: UNDELIVERED_MARK_SIZE,
    borderRadius: UNDELIVERED_MARK_SIZE / 2,
    borderWidth: 1.5,
    alignItems: 'center',
    justifyContent: 'center',
  },
  markGlyph: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 14,
    includeFontPadding: false,
    textAlign: 'center',
  },
});
