import { useEffect, useState } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import { useRouter } from "expo-router";
import { Typography, useAppTheme } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";
import { browserEnrollmentServer, readBrowserEnrollmentStatus, requestBrowserEnrollment, type BrowserEnrollmentPrompt } from "../../services/browserEnrollment";
import { markOnboarded, saveServer, setServerAutoConnect } from "../../services/storage";
import { useCurrentServer } from "../../store/currentServer";

export function BrowserEnrollmentScreen() {
  const { colors } = useAppTheme();
  const router = useRouter();
  const { refreshServers } = useCurrentServer();
  const [attempt, setAttempt] = useState(0);
  const [prompt, setPrompt] = useState<BrowserEnrollmentPrompt | null>(null);
  const [state, setState] = useState("starting");
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setState("starting");
    setPrompt(null);
    setError("");
    const origin = window.location.origin;
    async function poll(request: BrowserEnrollmentPrompt) {
      try {
        const status = await readBrowserEnrollmentStatus(origin, request);
        if (cancelled) return;
        if (status.status === "approved") {
          const input = browserEnrollmentServer(origin, status);
          if (!input) throw new Error("The approval response is incomplete. Please retry.");
          const server = await saveServer(input);
          await setServerAutoConnect(server.id, true);
          await markOnboarded();
          if (cancelled) return;
          await refreshServers(server.id);
          router.replace("/list");
          return;
        }
        setError("");
        setState(status.status);
        if (status.status === "pending") timer = setTimeout(() => void poll(request), 1500);
      } catch (err) {
        if (cancelled) return;
        if (Date.now() >= Date.parse(request.expiresAt)) setState("expired");
        else {
          setError("Connection interrupted. Retrying…");
          timer = setTimeout(() => void poll(request), 3000);
        }
      }
    }
    const cacheKey = "mewla:enrollment:v1";
    async function resumeOrRequest() {
      try {
        const cached = JSON.parse(sessionStorage.getItem(cacheKey) || "null");
        if (cached?.requestId && cached?.secret && Date.parse(cached.expiresAt) > Date.now()) return cached as BrowserEnrollmentPrompt;
      } catch { /* Private browsing may disable storage. */ }
      const request = await requestBrowserEnrollment(origin);
      try { sessionStorage.setItem(cacheKey, JSON.stringify(request)); } catch { /* In-memory flow still works. */ }
      return request;
    }
    void resumeOrRequest().then((request) => {
      if (cancelled) return;
      setPrompt(request);
      setState("pending");
      void poll(request);
    }).catch((err) => {
      if (cancelled) return;
      setState("error");
      setError(err instanceof Error ? err.message : "Could not request approval.");
    });
    return () => { cancelled = true; clearTimeout(timer); };
  }, [attempt, refreshServers, router]);

  const waiting = state === "pending" || state === "starting";
  return (
    <View style={[styles.page, { backgroundColor: colors.bgPrimary }]}>
      <View style={styles.content}>
        <AppText variant="caption" tone="secondary" style={styles.center}>Connect to Mewla</AppText>
        <AppText variant="display" accessibilityRole="header" style={styles.center}>{waiting ? "Waiting for approval" : state === "denied" ? "Request denied" : state === "expired" ? "Request expired" : "Could not connect"}</AppText>
        <AppText tone="secondary" style={styles.center}>{waiting ? "Approve this number on your computer or a paired phone." : state === "denied" ? "This browser wasn’t approved." : state === "expired" ? "This number expired." : error}</AppText>
        {waiting && prompt ? <AppText variant="display" accessibilityLabel={`Verification number ${prompt.verificationNumber}`} style={styles.number}>{prompt.verificationNumber}</AppText> : null}
        {waiting ? <ActivityIndicator color={colors.accent} /> : <Button label={state === "expired" ? "New number" : "Try again"} variant="filled" onPress={() => { try { sessionStorage.removeItem("mewla:enrollment:v1"); } catch {} setAttempt((value) => value + 1); }} />}
        {waiting && error ? <AppText tone="secondary" accessibilityLiveRegion="polite" style={styles.center}>{error}</AppText> : null}
        <AppText variant="caption" tone="secondary" style={styles.center}>First device? Run <Text style={styles.command}>mewla pair</Text> on your computer and open its link.</AppText>
        <Button label="Use a pairing link" variant="plain" onPress={() => router.push({ pathname: "/settings", params: { addServer: Date.now().toString(), pairingRequired: "1", pairMode: "import" } })} />
      </View>
    </View>
  );
}
const styles = StyleSheet.create({
  page: { flex: 1, justifyContent: "center", alignItems: "center", padding: 24 },
  content: { width: "100%", maxWidth: 440, gap: 24 },
  number: { fontSize: 64, lineHeight: 80, textAlign: "center", letterSpacing: 12 },
  center: { textAlign: "center" },
  command: { fontFamily: Typography.terminalFont },
});
