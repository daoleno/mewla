import { DateStyle, formatDate } from "../../services/dateFormat";
import React, { useEffect, useRef, useState } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppColors, TypeScale, UiTextMetrics } from "../../constants/tokens";
import { Button } from "../ui/Button";
import { ListRow, ListSection } from "../ui/ListSection";
import { Spinner } from "../ui/Spinner";
import { IconButton } from "../ui/IconButton";
import { InlineNotice } from "../ui/InlineNotice";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import type { TelegramConnectionStatus } from "../../services/websocket";
import { Icon, type IconName } from "../icons/Icon";

export interface TelegramConnectionPanelProps {
  status: TelegramConnectionStatus | null;
  connected: boolean;
  loading: boolean;
  busy: boolean;
  error: string | null;
  token: string;
  editingToken: boolean;
  onToken: (value: string) => void;
  onPaste: () => void;
  onConfigure: () => void;
  onBind: () => void;
  onOpen: () => void;
  onBotFather: () => void;
  onReconnect: () => void;
  onDisconnect: () => void;
  onEditToken: () => void;
  onCancelToken: () => void;
  onRevoke: () => void;
  onRemove: () => void;
  onRetry: () => void;
  /** Copies a diagnostic value; resolves true once it is on the clipboard. */
  onCopy: (value: string) => Promise<boolean>;
}

/**
 * Telegram detail page: identity, one primary next step, grouped secondary
 * actions, then diagnostics and destructive actions behind Advanced.
 */
export function TelegramConnectionPanel(props: TelegramConnectionPanelProps) {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const [advanced, setAdvanced] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (copiedTimer.current) clearTimeout(copiedTimer.current); }, []);
  const { status, connected, loading, busy, error } = props;
  const configured = Boolean(status?.bot_username);
  const bound = Boolean(status?.owner_hint);
  const disabled = busy || !connected || loading;
  const label = !connected ? "Server offline" : loading ? "Loading" : !configured ? "Not connected"
    : !status?.enabled ? "Disconnected" : !bound ? "Awaiting connection" : status.state === "degraded" ? "Needs attention" : "Connected";
  const labelInk = !connected || label === "Needs attention" ? colors.warning
    : label === "Connected" ? colors.success : colors.textSecondary;
  const recipient = status?.topics_available ? "Brain" : status?.recipient_label || (status?.recipient_id ? "Session unavailable" : "Brain");

  // A primary action is the one filled call to action; everything else is a
  // row in a grouped card.
  const primary = (name: string, icon: IconName, onPress: () => void, unavailable = disabled) => (
    <Button key={name} label={name} icon={icon} variant="filled" size="lg" block disabled={unavailable} onPress={onPress} />
  );
  const row = (name: string, icon: IconName, onPress: () => void, destructive = false) => (
    <ListRow key={name} title={name} icon={icon} destructive={destructive} disabled={disabled} onPress={onPress} />
  );
  const detail = (name: string, value: string) => <ListRow key={name} title={name} value={value} />;
  // Opaque identifiers keep both ends readable and copy in full on tap.
  const copyableDetail = (name: string, value: string) => {
    const done = copied === name;
    const copy = async () => {
      if (!(await props.onCopy(value))) return;
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
      setCopied(name);
      copiedTimer.current = setTimeout(() => setCopied(null), 1600);
    };
    return (
      <ListRow key={name} title={name} value={done ? "Copied" : value} accessibilityLabel={`${name} ${value}`}
        accessibilityHint={`Copies the ${name.toLowerCase()} ID`} onPress={() => void copy()} />
    );
  };
  const group = (children: React.ReactNode[], title?: string) => {
    const rows = children.filter(Boolean);
    return rows.length === 0 ? null : <ListSection title={title}>{rows}</ListSection>;
  };

  return <ScrollView keyboardShouldPersistTaps="handled" keyboardDismissMode="on-drag"
    contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 16) + 24 }]}>
    <View style={styles.identity}>
      <View style={[styles.icon, { backgroundColor: colors.accentSoft }]}>
        <Icon name="paper-plane" size={26} color={colors.accentStrong} />
      </View>
      <Text numberOfLines={1} style={[styles.title, { color: colors.textPrimary }]}>{configured ? `@${status?.bot_username}` : "Telegram"}</Text>
      <View style={styles.statusLine}>
        {loading || busy ? <Spinner /> : null}
        <Text style={[styles.subtitle, { color: labelInk }]}>{label}</Text>
      </View>
      {status?.owner_hint ? <Text numberOfLines={1} style={[styles.caption, { color: colors.textTertiary }]}>{status.owner_hint}</Text> : null}
    </View>

    {error || status?.last_error ? <InlineNotice tone="danger" title="Telegram problem" detail={error || status?.last_error} detailNumberOfLines={0} style={styles.gap} /> : null}

    {!connected ? <Text style={[styles.notice, { color: colors.textSecondary }]}>Reconnect the current server in Settings.</Text>
      : loading ? null : error ? primary("Retry", "refresh", props.onRetry)
      : !configured || props.editingToken ? <View style={styles.stack}>
        <ListSection title="Bot token" footer="Bot chats are stored by Telegram. Your token stays on the connected server." style={styles.flush}>
          <MobileSingleLineInput framed value={props.token} onChangeText={props.onToken} secureTextEntry editable={!disabled}
            accessibilityLabel="Telegram bot token" placeholder="Paste the BotFather token" autoCapitalize="none" autoCorrect={false}
            textContentType="none" importantForAutofill="no" returnKeyType="done" onSubmitEditing={props.onConfigure}
            placeholderTextColor={colors.textTertiary}
            trailing={<IconButton icon="clipboard" tone="ghost" size={44} iconSize={21} color={colors.accentStrong} disabled={disabled}
              accessibilityLabel="Paste Telegram bot token from clipboard" onPress={props.onPaste} />} />
        </ListSection>
        {primary(busy ? "Verifying" : "Verify token", "check", props.onConfigure, disabled || !props.token.trim())}
        {group([props.editingToken ? row("Cancel", "close", props.onCancelToken) : row("BotFather", "open-external", props.onBotFather)])}
      </View> : <View style={styles.stack}>
        {bound ? <>
          {status?.enabled ? primary("Open Telegram", "open-external", props.onOpen) : primary("Reconnect", "refresh", props.onReconnect)}
          {group([
            detail("Status", label),
            detail("Recipient", recipient),
            status?.enabled ? row("Disconnect", "pause", props.onDisconnect) : row("Open Telegram", "open-external", props.onOpen),
          ])}
        </> : <>
          {primary(status?.binding_pending ? "Open connection link" : "Connect Telegram", "open-external", props.onBind)}
          {group([detail("Status", label)])}
        </>}
      </View>}

    {configured && connected && !loading ? <>
      <ListSection style={styles.advanced}>
        <ListRow title="Advanced" expanded={advanced} accessory="chevron" accessibilityLabel="Advanced" onPress={() => setAdvanced(value => !value)} />
      </ListSection>
      {advanced ? <View style={styles.stack}>
        {group([
          detail("Chat mode", status?.topics_available ? "Native topics" : "Private chat"),
          status?.topic_mappings ? detail("Session topics", String(status.topic_mappings)) : null,
          status?.brain_thread_id ? copyableDetail("Brain conversation", status.brain_thread_id) : null,
          status?.brain_topic_id ? detail("Brain topic", String(status.brain_topic_id)) : null,
          detail("Delivery checks", String((status?.ambiguous_delivery_count || 0) + (status?.topic_ambiguous_ops_count || 0))),
          status?.last_receive_at ? detail("Last received", formatDate(status.last_receive_at, DateStyle.dateTime)) : null,
          status?.last_send_at ? detail("Last sent", formatDate(status.last_send_at, DateStyle.dateTime)) : null,
        ])}
        {group([
          row("Refresh", "refresh", props.onRetry),
          row("Replace token", "key", props.onEditToken),
        ])}
        {group([
          bound ? row("Unlink account", "person-remove", props.onRevoke, true) : null,
          row("Remove bot", "trash", props.onRemove, true),
        ])}
      </View> : null}
    </> : null}
  </ScrollView>;
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 16, flexGrow: 1 },
  identity: { alignItems: "center", gap: 4, paddingTop: 20, paddingBottom: 24 },
  icon: { width: 60, height: 60, borderRadius: 18, alignItems: "center", justifyContent: "center", marginBottom: 8 },
  title: { ...UiTextMetrics, ...TypeScale.heading, flexShrink: 1, textAlign: "center" },
  statusLine: { flexDirection: "row", alignItems: "center", gap: 6 },
  subtitle: { ...UiTextMetrics, ...TypeScale.compact, flexShrink: 1 },
  caption: { ...UiTextMetrics, ...TypeScale.caption },
  stack: { gap: 4 },
  gap: { marginBottom: 12 },
  flush: { marginBottom: 6 },
  notice: { ...UiTextMetrics, ...TypeScale.caption, paddingHorizontal: 16, marginTop: 8, marginBottom: 18 },
  advanced: { marginTop: 12, marginBottom: 8 },
});
