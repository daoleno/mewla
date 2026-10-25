import React, { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, AppState, Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useIsFocused, useRouter } from "expo-router";
import { WebView, type WebViewMessageEvent } from "react-native-webview";
import { catFocusScript, catStateScript, parseCatBridgeMessage } from "../../services/catPark";
import { cachedCatBundle, syncCatBundle, type CatBundle } from "../../services/catParkBundle";
import { reportCatSync } from "../../services/catParkReport";
import { catFailureCause, CatSyncError, type CatSyncProgress } from "../../services/catParkSync";
import { wsClient } from "../../services/websocket";
import { resolvePrimaryAppBarGeometry } from "../navigation/primaryAppBarGeometry";
import { useCatPark } from "./useCatPark";

const GROUND = "#2a211a";
/** A sync that fails early is run again (what arrived stays), this many runs in all. */
const SYNC_RUNS = 3;
/** No new run starts after this long: one that hit its deadline is not repeated behind the person's back. */
const SYNC_RERUN_WITHIN_MS = 60_000;
const SYNC_RERUN_PAUSE_MS = 1_500;

interface Failure {
  /** What went wrong, shown only when the computer itself is reachable; otherwise the link is the story. */
  reason: string | null;
}

/**
 * The cat's house, the App's third primary page (Brain · Sessions · Cat). It
 * sits under the primary app bar, so the page starts below it. The page is the
 * daemon's own bundle, synced
 * into the phone's cache and opened from disk: it holds no credentials, never
 * touches the network, and can only ask this screen for the few things the
 * bridge allows (see services/catPark.ts).
 */
export function CatParkScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { server, view, act } = useCatPark();
  const serverKey = server?.id ?? "";
  const [bundle, setBundle] = useState<CatBundle | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [progress, setProgress] = useState<CatSyncProgress | null>(null);
  const [attempt, setAttempt] = useState(0);
  const web = useRef<WebView>(null);
  const ready = useRef(false);
  const latest = useRef(view);
  latest.current = view;
  const focused = useIsFocused();
  const [foreground, setForeground] = useState(AppState.currentState === "active");
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (next) => setForeground(next === "active"));
    return () => subscription.remove();
  }, []);
  const onScreen = focused && foreground;
  const onScreenRef = useRef(onScreen);
  onScreenRef.current = onScreen;

  // Leaving the house is Brain, the first tab, wherever the tab was opened from.
  const leave = useCallback(() => {
    router.navigate("/");
  }, [router]);

  // Open what the phone already has at once, and refresh it for next time; the
  // first ever open waits for the download.
  useEffect(() => {
    ready.current = false;
    setBundle(null);
    setFailure(null);
    setProgress(null);
    if (!server) return;
    let cancelled = false;
    void (async () => {
      const cached = await cachedCatBundle(server.id).catch(() => null);
      if (cancelled) return;
      if (cached) {
        setBundle(cached);
        void syncCatBundle(server).catch(() => undefined);
        return;
      }
      const started = Date.now();
      let last: unknown = null;
      for (let run = 1; run <= SYNC_RUNS; run++) {
        try {
          const fresh = await syncCatBundle(server, (next) => {
            if (!cancelled) setProgress(next);
          });
          if (cancelled) return;
          setBundle(fresh);
          if (run > 1) reportCatSync(server.id, "cat_sync_recovered", `runs=${run} after=${Date.now() - started}ms last=${last instanceof CatSyncError ? last.diagnostic() : catFailureCause(last)}`);
          return;
        } catch (error) {
          last = error;
          if (cancelled) return;
          if ((error instanceof CatSyncError && error.refused) || Date.now() - started > SYNC_RERUN_WITHIN_MS) break;
          if (run < SYNC_RUNS) await new Promise<void>((resolve) => setTimeout(resolve, SYNC_RERUN_PAUSE_MS));
          if (cancelled) return;
        }
      }
      const reachable = wsClient.isConnected(server.id);
      reportCatSync(
        server.id,
        "cat_sync_failed",
        `${last instanceof CatSyncError ? last.diagnostic() : `stage=other error="${catFailureCause(last)}"`} reachable=${reachable} after=${Date.now() - started}ms`,
      );
      setFailure({ reason: reachable ? (last instanceof CatSyncError ? last.message : catFailureCause(last)) : null });
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverKey, attempt]);

  const push = useCallback(() => {
    const current = latest.current;
    if (ready.current && current) web.current?.injectJavaScript(catStateScript(current, true));
  }, []);
  useEffect(push, [view, push]);

  // The room rests and its sounds stop while the tab is not on screen.
  useEffect(() => {
    if (ready.current) web.current?.injectJavaScript(catFocusScript(onScreen));
  }, [onScreen]);

  const onMessage = useCallback((event: WebViewMessageEvent) => {
    const message = parseCatBridgeMessage(event.nativeEvent.data);
    if (!message) return;
    switch (message.type) {
      case "ready":
        ready.current = true;
        push();
        web.current?.injectJavaScript(catFocusScript(onScreenRef.current));
        break;
      case "back":
      case "open_brain":
        leave();
        break;
      default:
        act(message);
    }
  }, [act, leave, push]);

  if (!server || failure) {
    return (
      <View style={[styles.fill, styles.center]}>
        <Pressable accessibilityRole="button" onPress={server ? () => setAttempt((n) => n + 1) : leave} style={styles.retry}>
          <Text style={styles.retryText}>{server ? failure?.reason ?? "Can't reach your computer" : "No computer"}</Text>
          {server ? <Text style={styles.retryHint}>Tap to retry</Text> : null}
        </Pressable>
      </View>
    );
  }
  if (!bundle) {
    return (
      <View style={[styles.fill, styles.center]}>
        <ActivityIndicator color="#f3e9d8" />
        <Text style={styles.status}>
          {progress && progress.total > 0 && progress.done > 0
            ? `Bringing the house over… ${Math.round((progress.done / progress.total) * 100)}%`
            : "Bringing the house over…"}
        </Text>
      </View>
    );
  }
  return (
    <View style={[styles.fill, { paddingTop: resolvePrimaryAppBarGeometry(insets.top).contentInset, paddingBottom: insets.bottom }]}>
      <WebView
        key={bundle.version}
        ref={web}
        style={styles.fill}
        source={{ uri: bundle.indexUri }}
        originWhitelist={["file://*"]}
        onShouldStartLoadWithRequest={(request) => request.url.startsWith(bundle.dirUri)}
        allowFileAccess
        allowingReadAccessToURL={bundle.dirUri}
        allowFileAccessFromFileURLs={false}
        allowUniversalAccessFromFileURLs={false}
        javaScriptCanOpenWindowsAutomatically={false}
        setSupportMultipleWindows={false}
        domStorageEnabled={false}
        sharedCookiesEnabled={false}
        thirdPartyCookiesEnabled={false}
        cacheEnabled={false}
        incognito
        bounces={false}
        overScrollMode="never"
        scrollEnabled={false}
        setBuiltInZoomControls={false}
        accessibilityLabel="Cat"
        // No reset on load start: the page's "ready" can arrive before that
        // event, and a push into a page still loading is a no-op anyway.
        onMessage={onMessage}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1, backgroundColor: GROUND },
  center: { alignItems: "center", justifyContent: "center" },
  retry: { minHeight: 48, paddingHorizontal: 28, justifyContent: "center", alignItems: "center" },
  status: { color: "#f3e9d8", fontSize: 13, opacity: 0.7, marginTop: 14 },
  retryText: { color: "#f3e9d8", fontSize: 15, textAlign: "center" },
  retryHint: { color: "#f3e9d8", fontSize: 13, opacity: 0.6, marginTop: 6 },
});
