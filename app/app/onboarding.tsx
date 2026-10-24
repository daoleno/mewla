import React, { useRef, useState } from "react";
import { Alert, Platform } from "react-native";
import * as Clipboard from "expo-clipboard";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useCurrentServer } from "../store/currentServer";
import { useWorkerServerSummary } from "../store/workers";
import { wsClient } from "../services/websocket";
import { setServerAutoConnect } from "../services/storage";
import { importConnection } from "../services/importConnection";
import { PairingCancelledError } from "../services/pairingScope";
import { createPairScanClaim, releasePairScan, tryClaimPairScan } from "../services/pairScanClaim";
import { OnboardingPresentation } from "../components/onboarding/OnboardingPresentation";

import { BrowserEnrollmentScreen } from "../components/enrollment/BrowserEnrollmentScreen";
import { serverDisplayName } from "../services/serverDisplayName";

export default function OnboardingScreen() {
  const router = useRouter();
  const { paired } = useLocalSearchParams<{ paired?: string }>();
  const { currentServer, isCurrentServer, refreshServers } = useCurrentServer();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const [pairedHere, setPairedHere] = useState(false);
  const [pairing, setPairing] = useState(false);
  // Camera frames and Paste share one latch, as in Settings: one import at a time.
  const scanClaim = useRef(createPairScanClaim());
  // A QR code that is not a Mewla code stays in view; say so once, not every frame.
  const rejected = useRef<string | null>(null);
  const server = paired === "1" || pairedHere ? currentServer : null;
  if (Platform.OS === "web" && !currentServer) return <BrowserEnrollmentScreen />;

  const pair = async (rawValue: string, fromCamera: boolean) => {
    const value = rawValue.trim();
    if (fromCamera && value === rejected.current) return;
    if (!tryClaimPairScan(scanClaim.current)) return;
    setPairing(true);
    try {
      const saved = await importConnection(value, {
        onImported: async (importedServer) => {
          await refreshServers(importedServer.id);
        },
      });
      if (!saved) {
        rejected.current = value;
        Alert.alert("Not a pairing code", "Scan the QR code mewla shows, or paste the link from mewla pair --details.");
        return;
      }
      setPairedHere(true);
    } catch (error) {
      if (error instanceof PairingCancelledError) return;
      rejected.current = value;
      Alert.alert("Pairing failed", error instanceof Error ? error.message : "Could not pair with that computer.");
    } finally {
      releasePairScan(scanClaim.current);
      setPairing(false);
    }
  };

  return (
    <OnboardingPresentation
      serverName={server ? serverDisplayName(server.name, "your computer") : undefined}
      connection={server ? serverConnections[server.id] ?? "connecting" : undefined}
      error={server ? serverConnectionIssues[server.id]?.detail : undefined}
      pairing={pairing}
      onCode={(data) => void pair(data, true)}
      onPaste={() => void Clipboard.getStringAsync().then((text) => {
        if (text.trim()) return pair(text, false);
        Alert.alert("Nothing to paste", "Copy the link from mewla pair --details on your computer, then tap Paste again.");
      })}
      onContinue={() => router.replace("/")}
      onSettings={() => router.push("/settings")}
      onRetry={() => {
        if (!server || !isCurrentServer(server.id)) return;
        void setServerAutoConnect(server.id, true).then(() => {
          if (isCurrentServer(server.id)) wsClient.connect(server);
        }).catch((error) => Alert.alert("Connection failed", error instanceof Error ? error.message : "Could not retry this server."));
      }}
    />
  );
}
