/** Standalone Metro fixture entry. Not an Expo Router route or a live-data fallback. */
import React from "react";
import { useFonts } from "expo-font";
import { registerRootComponent } from "expo";
import { View } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { ThemeProvider } from "../../theme/provider";
import { ResourcesView } from "../../components/resources/ResourcesView";
import { normalizeResourceTelemetry } from "../../services/resourceTelemetry";
import fixture from "./dashboard-v2.json";

const telemetry = normalizeResourceTelemetry(fixture)!;
function ResourcesPreview() {
  const [loaded] = useFonts({
    "SourceHanSansSC-Regular": require("../../assets/fonts/SourceHanSansSC-Regular.otf"),
    "SourceHanSansSC-Medium": require("../../assets/fonts/SourceHanSansSC-Medium.otf"),
    "MapleMono-CN-Regular": require("../../assets/fonts/MapleMono-CN-Regular.ttf"),
    "MapleMono-CN-SemiBold": require("../../assets/fonts/MapleMono-CN-SemiBold.ttf"),
  });
  if (!loaded) return null;
  return <SafeAreaProvider><ThemeProvider><View style={{ flex: 1 }}>
    <ResourcesView telemetry={telemetry} loading={false} error={null} connected hasServer 
      now={telemetry.sampledAt + 1000} onRetry={() => {}} />
  </View></ThemeProvider></SafeAreaProvider>;
}
registerRootComponent(ResourcesPreview);
