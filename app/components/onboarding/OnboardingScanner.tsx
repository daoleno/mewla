import React, { useEffect, useState } from "react";
import { AppState, Linking, Platform, StyleSheet, View } from "react-native";
import { CameraView, useCameraPermissions } from "expo-camera";
import { useAppColors } from "../../constants/tokens";
import { AppText } from "../ui/AppText";
import { Button } from "../ui/Button";

/**
 * The viewfinder on the scan step. The camera runs only while this step is on
 * screen and the app is in front; `paused` stops reading while a code pairs.
 */
export function OnboardingScanner({ paused, onCode }: { paused: boolean; onCode(data: string): void }) {
  const colors = useAppColors();
  const [permission, requestPermission] = useCameraPermissions();
  const [foreground, setForeground] = useState(AppState.currentState === "active");
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (next) => setForeground(next === "active"));
    return () => subscription.remove();
  }, []);

  const camera = Platform.OS !== "web" && permission?.granted && foreground;
  return (
    <View style={[styles.finder, { borderColor: colors.border }]}>
      {camera ? (
        <CameraView
          style={StyleSheet.absoluteFill}
          facing="back"
          barcodeScannerSettings={{ barcodeTypes: ["qr"] }}
          onBarcodeScanned={paused ? undefined : ({ data }) => onCode(data || "")}
        />
      ) : permission && !permission.granted && Platform.OS !== "web" ? (
        <View style={styles.ask}>
          <AppText variant="compact" style={styles.askText}>
            {permission.canAskAgain ? "Mewla reads the code with the camera." : "The camera is off for Mewla. Turn it on in Settings, or paste the link instead."}
          </AppText>
          <Button
            size="sm"
            variant="tinted"
            icon="camera"
            label={permission.canAskAgain ? "Allow camera" : "Open Settings"}
            onPress={() => void (permission.canAskAgain ? requestPermission() : Linking.openSettings())}
          />
        </View>
      ) : null}
      {(["tl", "tr", "bl", "br"] as const).map((corner) => (
        <View key={corner} style={[styles.corner, styles[corner]]} />
      ))}
    </View>
  );
}

const ARM = 26;
const styles = StyleSheet.create({
  // The finder is a dark window in both themes, like the camera behind it.
  finder: { backgroundColor: "#0A0908", borderWidth: StyleSheet.hairlineWidth, width: "100%", aspectRatio: 1, maxHeight: 320, alignSelf: "center", borderRadius: 24, overflow: "hidden", justifyContent: "center" },
  ask: { alignItems: "center", gap: 14, paddingHorizontal: 28 },
  askText: { color: "#F4F0EA", textAlign: "center" },
  corner: { position: "absolute", width: ARM, height: ARM, borderColor: "#F4F0EA", opacity: 0.9 },
  tl: { top: 18, left: 18, borderTopWidth: 3, borderLeftWidth: 3, borderTopLeftRadius: 10 },
  tr: { top: 18, right: 18, borderTopWidth: 3, borderRightWidth: 3, borderTopRightRadius: 10 },
  bl: { bottom: 18, left: 18, borderBottomWidth: 3, borderLeftWidth: 3, borderBottomLeftRadius: 10 },
  br: { bottom: 18, right: 18, borderBottomWidth: 3, borderRightWidth: 3, borderBottomRightRadius: 10 },
});
