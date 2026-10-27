import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, ScrollView, StyleSheet, View } from "react-native";
import { Redirect, Stack, useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ContentWidth, useAppColors } from "../../constants/tokens";
import { ComputerPanel } from "../../components/settings/ComputerPanel";
import {
  computerProblems,
  computerProblemsMayChange,
  computerStatusWord,
  type ComputerAction,
  type ComputerProblem,
} from "../../components/settings/computerModel";
import { confirmDestructive } from "../../components/ui";
import { changeAgentUse, getAgentUse, type AgentUseChange, type AgentUseStatus } from "../../services/agentUse";
import { cancelCalendarNotifications } from "../../services/calendarNotifications";
import {
  checkComputer,
  computersApartFrom,
  forgetEverywhere,
  joiningComputers,
  type ComputerCheck,
} from "../../services/computerTeam";
import { switchToComputer } from "../../services/computerSwitch";
import { revokeThisDevice } from "../../services/deviceRevoke";
import { invalidateStoredServerTransport } from "../../services/pinnedTransport";
import * as Storage from "../../services/storage";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkerServerSummary } from "../../store/workers";

const POLL_MS = 5_000;

export default function ComputerScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const colors = useAppColors();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { currentServerId, refreshServers, switchCurrentServer } = useCurrentServer();
  const { dispatch, serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const [servers, setServers] = useState<Storage.StoredServer[] | null>(null);
  const [check, setCheck] = useState<ComputerCheck | undefined>(undefined);
  const [agentUse, setAgentUse] = useState<AgentUseStatus | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [note, setNote] = useState<{ key: string; text: string } | null>(null);
  const [draftName, setDraftName] = useState("");
  const [draftAddress, setDraftAddress] = useState("");

  const server = useMemo(() => servers?.find((candidate) => candidate.id === id) ?? null, [id, servers]);
  const serverRef = useRef(server);
  serverRef.current = server;
  const current = Boolean(server && server.id === currentServerId);
  const connection = server ? serverConnections[server.id] : undefined;
  const issue = server ? serverConnectionIssues[server.id] ?? null : null;
  const reachable = (current && connection === "connected") || check?.state === "online";

  const loadServers = useCallback(async () => {
    const saved = await Storage.getServers();
    setServers(saved);
    const found = saved.find((candidate) => candidate.id === id);
    if (found) {
      setDraftName(found.name);
      setDraftAddress(Storage.serverAddressForDisplay(found.url));
    }
  }, [id]);

  useFocusEffect(
    useCallback(() => {
      void loadServers();
    }, [loadServers]),
  );

  // Ask the computer itself, current or not: a computer that isn't in use is
  // not offline until it fails to answer.
  const serverKey = server ? `${server.id}:${server.url}` : "";
  useEffect(() => {
    if (!serverRef.current) return;
    const target = serverRef.current;
    let cancelled = false;
    setCheck(undefined);
    void checkComputer(target).then(async (result) => {
      if (cancelled) return;
      setCheck(result);
      if (result.state === "online" && result.name && Storage.serverNamedByAddress(target)) {
        await Storage.updateServer(target.id, { name: result.name });
        await refreshServers();
        await loadServers();
      }
    });
    return () => {
      cancelled = true;
      // Close the Link tunnel this page opened, unless the app now uses it
      // or computers are still being introduced over it.
      if (wsClient.liveServerId() !== target.id && !joiningComputers()) void invalidateStoredServerTransport(target);
    };
  }, [serverKey, loadServers, refreshServers]);

  const agentUseRef = useRef<AgentUseStatus | null>(null);
  agentUseRef.current = agentUse;
  const loadAgentUse = useCallback(async () => {
    const target = serverRef.current;
    if (!target) return;
    try {
      const next = await getAgentUse(target);
      if (serverRef.current?.id === target.id) setAgentUse(next);
    } catch (reason) {
      // A missed look while the page polls keeps what it last showed.
      if (serverRef.current?.id === target.id && !agentUseRef.current) {
        setNote({ key: "agents", text: reason instanceof Error ? reason.message : String(reason) });
      }
    }
  }, []);

  useEffect(() => {
    setAgentUse(null);
    setNote(null);
    if (reachable) void loadAgentUse();
  }, [reachable, serverKey, loadAgentUse]);

  // While someone is fixing something on the computer, look again now and then.
  const mayChange = reachable && computerProblemsMayChange(agentUse);
  useEffect(() => {
    if (!mayChange) return;
    const timer = setInterval(() => void loadAgentUse(), POLL_MS);
    return () => clearInterval(timer);
  }, [mayChange, loadAgentUse]);

  if (!servers) return null;
  if (!server) {
    // A removed computer, or a link without one: back to the list, not a blank page.
    return <Redirect href="/settings" />;
  }

  const apply = async (key: string, change: AgentUseChange, after?: string) => {
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setBusy(key);
    setNote(null);
    try {
      const next = await changeAgentUse(server, change);
      if (serverRef.current?.id !== server.id) return;
      setAgentUse(next);
      if (after) setNote({ key, text: after });
    } catch (reason) {
      setNote({ key, text: reason instanceof Error ? reason.message : String(reason) });
    } finally {
      setBusy(null);
    }
  };

  const onAction = (problem: ComputerProblem, action: ComputerAction) => {
    switch (action.kind) {
      case "stop_agent":
        void apply(problem.key, { stop_agent: true });
        return;
      case "paste_key":
        void (async () => {
          const key = (await Clipboard.getStringAsync()).trim();
          if (!key) {
            setNote({ key: problem.key, text: "Copy the key the extension shows, then tap Paste key." });
            return;
          }
          await apply(problem.key, { key });
        })();
        return;
      case "fix":
        void apply(
          problem.key,
          { fix: action.fix },
          action.fix === "install_extension"
            ? `Click Add to Chrome on ${server.name}.`
            : action.fix === "open_settings"
              ? `Turn on Mewla in the settings that opened on ${server.name}.`
              : undefined,
        );
        return;
    }
  };

  const others = servers.filter((candidate) => candidate.id !== server.id);
  const apartNames = computersApartFrom(server)
    .map((daemonId) => others.find((other) => other.daemonId === daemonId)?.name)
    .filter((name): name is string => Boolean(name));
  const problems: ComputerProblem[] = [
    ...(reachable ? computerProblems(agentUse) : []),
    ...apartNames.map((name) => ({
      key: `apart:${name}`,
      text: `Can't reach ${name}. Give one of them a Tailscale or HTTPS address.`,
    })),
  ];

  const pairAgain = () => router.push({ pathname: "/settings", params: { addServer: Date.now().toString() } });
  const reconnect = () => {
    void Storage.setServerAutoConnect(server.id, true);
    wsClient.connect(server);
  };
  const status = computerStatusWord({ current, connection, issue: issue?.code, check });
  const connectionProblem =
    status === "Pair again"
      ? { text: "This computer no longer knows this phone.", label: "Pair again", onPress: pairAgain }
      : current && issue
        ? { text: issue.title, detail: issue.hint || issue.detail, label: "Try again", onPress: reconnect }
        : status === "Not connected"
          ? { text: "Not connected", label: "Connect", onPress: reconnect }
          : null;

  const saveDetails = async () => {
    const name = draftName.trim();
    const address = draftAddress.trim();
    const moved = server.transportKind !== "link" && address !== Storage.serverAddressForDisplay(server.url);
    if (moved) {
      const problem = Storage.serverAddressProblem(address);
      if (problem) {
        Alert.alert("Check the address", problem);
        return;
      }
    }
    if ((!name || name === server.name) && !moved) {
      setDraftName(server.name);
      return;
    }
    let saved: Storage.StoredServer | null;
    try {
      saved = await Storage.updateServer(server.id, {
        ...(name && name !== server.name ? { name } : {}),
        ...(moved ? { url: address } : {}),
      });
    } catch (reason) {
      Alert.alert("Check the address", reason instanceof Error ? reason.message : String(reason));
      return;
    }
    await refreshServers();
    await loadServers();
    if (saved && moved && current) {
      await Storage.setServerAutoConnect(saved.id, true);
      wsClient.connect(saved);
    }
  };

  const remove = () =>
    confirmDestructive({
      title: `Remove ${server.name}?`,
      message: "This phone and your other computers forget it. Its Sessions stay there. Pair again to bring it back.",
      confirmLabel: "Remove",
      onConfirm: async () => {
        setBusy("remove");
        try {
          // While the computers still trust this phone; each step is bounded
          // so an offline computer never blocks the removal.
          const missed = await forgetEverywhere(server, others);
          await revokeThisDevice(server);
          wsClient.disconnect(server.id);
          dispatch({ type: "REMOVE_SERVER", serverId: server.id });
          await Storage.removeServer(server.id);
          await cancelCalendarNotifications(server.id);
          // Removing the computer in use is the person's choice to move to
          // the next one; any other removal leaves the current computer as is.
          await refreshServers(current ? others[0]?.id : undefined);
          if (missed.length > 0) {
            const names = missed.map((other) => other.name).join(", ");
            Alert.alert(
              `${names} didn't answer`,
              `It still works with ${server.name}. On it, run mewla computers remove "${server.name}".`,
            );
          }
          router.back();
        } finally {
          setBusy(null);
        }
      },
    });

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: colors.bgPrimary }}
      contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 20) + 12 }]}
      keyboardShouldPersistTaps="handled"
      showsVerticalScrollIndicator={false}
    >
      <Stack.Screen options={{ title: server.name }} />
      <View style={styles.inner}>
        <ComputerPanel
          reachable={reachable}
          agentsEnabled={agentUse ? agentUse.enabled : null}
          problems={problems}
          connectionProblem={connectionProblem}
          note={note}
          busy={busy}
          name={draftName}
          onName={setDraftName}
          address={server.transportKind === "link" ? null : draftAddress}
          onAddress={setDraftAddress}
          onSaveDetails={() => void saveDetails()}
          onToggle={(enabled) => void apply("enabled", { enabled })}
          onAction={onAction}
          onUse={
            current
              ? undefined
              : async () => {
                  void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  await switchToComputer(server.id, switchCurrentServer);
                }
          }
          onRemove={remove}
        />
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: {
    width: "100%",
    paddingHorizontal: 16,
    paddingTop: 20,
  },
  inner: {
    width: "100%",
    maxWidth: ContentWidth.page,
    alignSelf: "center",
  },
});
