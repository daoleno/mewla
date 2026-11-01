import React, { useEffect, useState } from "react";
import { ActivityIndicator, AppState, Linking, Platform, Pressable, StyleSheet, View } from "react-native";
import { CameraView, useCameraPermissions } from "expo-camera";
import { useAppColors } from "../../constants/tokens";
import { cameraGate, cameraIsSearching, cameraMayRun } from "../../services/cameraGate";
import { AppText } from "../ui/AppText";

/**
 * The viewfinder on the scan step. The camera runs only while this step is on
 * screen and the app is in front; `paused` stops reading while a code pairs.
 */
export function OnboardingScanner({ paused, compact = false, onCode, onSearching }: { paused: boolean; /** A short window: a smaller viewfinder. */ compact?: boolean; onCode(data: string): void; /** Whether the camera is really looking for a code (not asking, denied or failed). */ onSearching?(searching: boolean): void }) {
  const colors = useAppColors();
  // The first time this step shows, the system asks for the camera on its own:
  // a viewfinder that waits for a tap on a dark window reads as a black screen.
  const [permission, requestPermission] = useCameraPermissions({ request: true });
  const [mountError, setMountError] = useState<string | null>(null);
  const [foreground, setForeground] = useState(cameraMayRun(AppState.currentState ?? "active"));
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (next) => setForeground(cameraMayRun(next)));
    return () => subscription.remove();
  }, []);

  const gate = Platform.OS === "web" ? "loading" : cameraGate(permission);
  const openSettings = () => void Linking.openSettings();
  const searching = cameraIsSearching(gate, mountError, foreground);
  useEffect(() => {
    onSearching?.(searching);
  }, [onSearching, searching]);
  return (
    <View style={[styles.finder, compact && styles.finderCompact, { borderColor: colors.border }]}>
      {mountError ? (
        <View style={styles.ask}>
          <AppText variant="compact" style={styles.askText}>
            The camera could not start. Check that Mewla may use it in Settings, or paste the link instead.
          </AppText>
          <FinderButton label="Open Settings" onPress={openSettings} />
        </View>
      ) : gate === "camera" && foreground ? (
        <CameraView
          style={StyleSheet.absoluteFill}
          facing="back"
          barcodeScannerSettings={{ barcodeTypes: ["qr"] }}
          onBarcodeScanned={paused ? undefined : ({ data }) => onCode(data || "")}
          onMountError={(event) => setMountError(event?.message || "camera")}
        />
      ) : gate === "request" ? (
        <View style={styles.ask}>
          <AppText variant="compact" style={styles.askText}>
            Mewla reads the code with the camera.
          </AppText>
          <FinderButton label="Allow camera" onPress={() => void requestPermission()} />
        </View>
      ) : gate === "denied" ? (
        <View style={styles.ask}>
          <AppText variant="compact" style={styles.askText}>
            The camera is off for Mewla. Turn it on in Settings, or paste the link instead.
          </AppText>
          <FinderButton label="Open Settings" onPress={openSettings} />
        </View>
      ) : gate === "loading" && Platform.OS !== "web" ? (
        <View style={styles.ask}>
          <ActivityIndicator color="#F4F0EA" />
        </View>
      ) : null}
      {(["tl", "tr", "bl", "br"] as const).map((corner) => (
        <View key={corner} style={[styles.corner, styles[corner]]} />
      ))}
    </View>
  );
}

/**
 * The finder is a dark window in both themes, so its button is light on dark in
 * both: a themed tinted button was nearly invisible on it in the light theme.
 */
function FinderButton({ label, onPress }: { label: string; onPress(): void }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label} onPress={onPress} style={({ pressed }) => [styles.finderButton, pressed && { opacity: 0.8 }]}>
      <AppText variant="compact" style={styles.finderButtonText}>{label}</AppText>
    </Pressable>
  );
}

const ARM = 26;
const styles = StyleSheet.create({
  // The finder is a dark window in both themes, like the camera behind it.
  finder: { backgroundColor: "#0A0908", borderWidth: StyleSheet.hairlineWidth, width: "100%", aspectRatio: 1, maxHeight: 320, alignSelf: "center", borderRadius: 24, overflow: "hidden", justifyContent: "center" },
  finderCompact: { maxWidth: 200, alignSelf: "flex-start" },
  ask: { alignItems: "center", gap: 14, paddingHorizontal: 28 },
  askText: { color: "#F4F0EA", textAlign: "center" },
  finderButton: { backgroundColor: "#F4F0EA", borderRadius: 999, paddingHorizontal: 18, paddingVertical: 10, minHeight: 44, justifyContent: "center" },
  finderButtonText: { color: "#0A0908", fontWeight: "600" },
  corner: { position: "absolute", width: ARM, height: ARM, borderColor: "#F4F0EA", opacity: 0.9 },
  tl: { top: 18, left: 18, borderTopWidth: 3, borderLeftWidth: 3, borderTopLeftRadius: 10 },
  tr: { top: 18, right: 18, borderTopWidth: 3, borderRightWidth: 3, borderTopRightRadius: 10 },
  bl: { bottom: 18, left: 18, borderBottomWidth: 3, borderLeftWidth: 3, borderBottomLeftRadius: 10 },
  br: { bottom: 18, right: 18, borderBottomWidth: 3, borderRightWidth: 3, borderBottomRightRadius: 10 },
});
