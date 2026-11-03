import React, { useCallback, useEffect, useRef, useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useFocusEffect } from "expo-router";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";
import { confirmDestructive } from "../ui";
import { ListRow, ListSection } from "../ui";
import { ContentWidth, useAppTheme } from "../../constants/tokens";
import {
  decideOutsideAgent,
  listOutsideAgents,
  type ApprovedAgent,
  type OutsideAgents,
  type PendingAgent,
} from "../../services/outsideAgents";
import { useCurrentServer } from "../../store/currentServer";

const POLL_MS = 3_000;

/** Settings → Outside agents: who asks to hand work to Brain, and who may. */
export function OutsideAgentsSettings() {
  const { colors } = useAppTheme();
  const insets = useSafeAreaInsets();
  const { currentServer } = useCurrentServer();
  const [state, setState] = useState<OutsideAgents | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const serverRef = useRef(currentServer);
  serverRef.current = currentServer;

  const refresh = useCallback(async () => {
    const server = serverRef.current;
    if (!server) return;
    try {
      setState(await listOutsideAgents(server));
      setError(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }, []);

  // New requests appear while this page is open; stop asking when it is not.
  useFocusEffect(
    useCallback(() => {
      void refresh();
      const timer = setInterval(() => void refresh(), POLL_MS);
      return () => clearInterval(timer);
    }, [refresh]),
  );
  useEffect(() => {
    setState(null);
    void refresh();
  }, [currentServer?.id, refresh]);

  const decide = async (action: "approve" | "deny" | "revoke", id: string) => {
    const server = serverRef.current;
    if (!server) return;
    setBusy(id);
    try {
      setState(await decideOutsideAgent(server, action, id));
      setError(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
      void refresh();
    } finally {
      setBusy(null);
    }
  };

  const revoke = (agent: ApprovedAgent) =>
    confirmDestructive({
      title: `Revoke ${agent.name}?`,
      message: "It can no longer hand work to Brain. Connect it again to allow it.",
      confirmLabel: "Revoke",
      onConfirm: () => decide("revoke", agent.id),
    });

  const pending = state?.pending ?? [];
  const agents = state?.agents ?? [];

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
    >
      <View style={styles.inner}>
        {!currentServer ? (
          <ListSection>
            <ListRow title="No current server" icon="desktop" disabled />
          </ListSection>
        ) : null}
        {error ? (
          <AppText style={[styles.note, { color: colors.dangerText }]} accessibilityLiveRegion="polite">
            {error}
          </AppText>
        ) : null}
        {pending.length > 0 ? (
          <ListSection>
            {pending.map((request) => (
              <PendingRow
                key={request.id}
                request={request}
                busy={busy === request.id}
                disabled={busy !== null}
                onApprove={() => decide("approve", request.id)}
                onDeny={() => decide("deny", request.id)}
              />
            ))}
          </ListSection>
        ) : null}
        {agents.length > 0 ? (
          <ListSection>
            {agents.map((agent) => (
              <ListRow
                key={agent.id}
                title={agent.name}
                subtitle={agent.last_used_at ? `Used ${new Date(agent.last_used_at).toLocaleString()}` : "Not used yet"}
                icon="brain"
                trailing={
                  <Button label="Revoke" size="sm" variant="destructive" disabled={busy !== null} onPress={() => revoke(agent)} />
                }
              />
            ))}
          </ListSection>
        ) : null}
        {currentServer && state && pending.length === 0 && agents.length === 0 ? (
          <AppText style={[styles.note, { color: colors.textSecondary }]}>
            On the computer where Claude Code or Codex runs: mewla agents mcp connect
          </AppText>
        ) : null}
      </View>
    </ScrollView>
  );
}

function PendingRow({
  request,
  busy,
  disabled,
  onApprove,
  onDeny,
}: {
  request: PendingAgent;
  busy: boolean;
  disabled: boolean;
  onApprove: () => void;
  onDeny: () => void;
}) {
  return (
    <ListRow
      title={request.name}
      subtitle={`Wants to hand work to Brain. Approve only if its terminal shows ${request.number}.`}
      subtitleNumberOfLines={3}
      icon="brain"
      trailing={
        <View style={styles.actions}>
          <Button label="Deny" size="sm" variant="tinted" disabled={disabled} onPress={onDeny} />
          <Button label="Approve" size="sm" loading={busy} disabled={disabled} onPress={onApprove} />
        </View>
      }
    />
  );
}

const styles = StyleSheet.create({
  content: { width: "100%", paddingHorizontal: 16, paddingTop: 16 },
  inner: { width: "100%", maxWidth: ContentWidth.page, alignSelf: "center", gap: 12 },
  note: { fontSize: 14, paddingHorizontal: 4 },
  actions: { flexDirection: "row", gap: 8 },
});
