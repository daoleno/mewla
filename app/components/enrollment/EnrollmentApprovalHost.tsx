import { useEffect, useMemo, useState } from "react";
import { AppState, Modal, ScrollView, StyleSheet, View } from "react-native";
import { useCurrentServer } from "../../store/currentServer";
import { wsClient } from "../../services/websocket";
import { decideEnrollment, decodePendingEnrollment, pendingEnrollments } from "../../services/enrollmentAPI";
import { verificationChoices, type PendingEnrollment } from "../../services/enrollmentApproval";
import { useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";

export function EnrollmentApprovalHost() {
  const { currentServer, isCurrentServer } = useCurrentServer();
  const [requests, setRequests] = useState<Array<PendingEnrollment & { serverId: string }>>([]);
  useEffect(() => {
    setRequests([]);
    if (!currentServer) return;
    let active = true;
    const add = (request: PendingEnrollment) => setRequests((items) => [...items.filter((item) => item.id !== request.id), { ...request, serverId: currentServer.id }]);
    const incoming = (data: any) => {
      if (!active || data.serverId !== currentServer.id) return;
      const request = decodePendingEnrollment(data);
      if (request) add(request);
    };
    const decision = (data: any) => {
      if (data.serverId === currentServer.id) setRequests((items) => items.filter((item) => item.id !== data.request_id));
    };
    const refresh = () => void pendingEnrollments(currentServer).then((pending) => {
      if (active && isCurrentServer(currentServer.id)) setRequests(pending.map((request) => ({ ...request, serverId: currentServer.id })));
    }).catch(() => {});
    const connected = (data: any) => { if (data.serverId === currentServer.id) refresh(); };
    const disconnected = (data: { serverId: string }) => { if (data.serverId === currentServer.id) setRequests([]); };
    wsClient.on("disconnected", disconnected);
    wsClient.on("enrollment_request", incoming);
    wsClient.on("enrollment_decision", decision);
    wsClient.on("connected", connected);
    const subscription = AppState.addEventListener("change", (state) => { if (state === "active") refresh(); });
    refresh();
    const timer = setInterval(() => setRequests((items) => items.filter((item) => Date.parse(item.expiresAt) > Date.now())), 1000);
    return () => {
      active = false;
      clearInterval(timer);
      subscription.remove();
      wsClient.off("disconnected", disconnected);
      wsClient.off("enrollment_request", incoming);
      wsClient.off("enrollment_decision", decision);
      wsClient.off("connected", connected);
    };
  }, [currentServer, isCurrentServer]);
  const request = requests.find((item) => item.serverId === currentServer?.id);
  if (!request || !currentServer) return null;
  return <EnrollmentApprovalSheet key={request.id} request={request} onDecide={async (number, approve) => {
    if (!isCurrentServer(currentServer.id)) return;
    await decideEnrollment(currentServer, request, number, approve);
    setRequests((items) => items.filter((item) => item.id !== request.id));
  }} />;
}

export function EnrollmentApprovalSheet({ request, onDecide }: { request: PendingEnrollment; onDecide(number: string, approve: boolean): Promise<void> }) {
  const { colors } = useAppTheme();
  const choices = useMemo(() => verificationChoices(request.verificationNumber), [request.id, request.verificationNumber]);
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function decide(approve: boolean) {
    setBusy(true);
    setError("");
    try { await onDecide(approve ? selected : request.verificationNumber, approve); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not send decision."); }
    finally { setBusy(false); }
  }
  return <Modal transparent animationType="fade" visible onRequestClose={() => {}}>
    <View style={styles.backdrop}>
      <View accessibilityViewIsModal style={[styles.sheet, { backgroundColor: colors.bgPrimary }]}>
        <ScrollView contentContainerStyle={styles.content}>
          <AppText variant="sheetTitle" accessibilityRole="header">Approve new device</AppText>
          <AppText variant="heading">{request.deviceName}</AppText>
          <AppText tone="secondary">{request.platform} · {request.origin}</AppText>
          <AppText>Choose the number shown on the new device. Only approve a request you started.</AppText>
          <View style={styles.choices}>{choices.map((number) => <Button key={number} label={number} haptic={false} accessibilityState={{ selected: selected === number }} variant={selected === number ? "filled" : "outlined"} disabled={busy} onPress={() => setSelected(number)} />)}</View>
          {error ? <AppText tone="danger" accessibilityRole="alert">{error}</AppText> : null}
          <Button label="Approve device" variant="filled" disabled={!selected || busy} loading={busy} onPress={() => void decide(true)} />
          <Button label="Deny" variant="destructive" disabled={busy} onPress={() => void decide(false)} />
        </ScrollView>
      </View>
    </View>
  </Modal>;
}
const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "rgba(0,0,0,0.45)", justifyContent: "center", alignItems: "center", padding: 24 },
  sheet: { maxWidth: 480, width: "100%", maxHeight: "90%", borderRadius: 24 },
  content: { padding: 24, gap: 20 },
  choices: { flexDirection: "row", flexWrap: "wrap", gap: 10 },
});
