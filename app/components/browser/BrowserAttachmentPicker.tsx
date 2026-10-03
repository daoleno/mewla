import React, { useEffect, useState } from "react";
import { Pressable, Text, View } from "react-native";
import { useCurrentServer } from "../../store/currentServer";
import { useAppTheme } from "../../constants/tokens";
import { browserRequest, type BrowserResource } from "../../services/browser";

// Selected anew for each launch. Never carries a profile across server switches.
export function BrowserAttachmentPicker({ value, onChange }: {
  value?: string;
  onChange(value?: string): void;
}) {
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { colors } = useAppTheme();
  const [resources, setResources] = useState<BrowserResource[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setResources([]);
    setError("");
    if (currentServer) {
      const server = currentServer;
      void browserRequest(server, { action: "list" }, controller.signal).then((result) => {
        if (controller.signal.aborted || !isCurrentServer(server.id)) return;
        setResources((result.resources || []).filter((r) => r.allow_agents && r.state === "running"));
      }).catch(() => { if (!controller.signal.aborted) setError("Browser list unavailable. You can continue without one."); });
    }
    return () => controller.abort();
  }, [currentServer, isCurrentServer]);
  return <View style={{ gap: 8 }}>
    <Text style={{ color: colors.textSecondary }}>Browser · optional for Codex or Claude</Text>
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
      {[{ id: "", name: "None" }, ...resources].map((resource) => <Pressable
        key={resource.id} accessibilityRole="radio" accessibilityState={{ selected: (value || "") === resource.id }}
        onPress={() => onChange(resource.id || undefined)}
        style={{ padding: 12, borderRadius: 12, backgroundColor: (value || "") === resource.id ? colors.surfaceSubtle : colors.bgSurface }}>
        <Text style={{ color: colors.textPrimary }}>{(value || "") === resource.id ? "✓ " : ""}{resource.name}</Text>
      </Pressable>)}
    </View>
    <Text style={{ color: colors.textSecondary, fontSize: 12 }}>{error || "Open a profile and enable managed Agents in Browser first. This session shares that host browser's sign-ins."}</Text>
  </View>;
}
