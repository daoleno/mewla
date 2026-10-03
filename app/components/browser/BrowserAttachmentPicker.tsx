import React, { useEffect, useState } from "react";
import { Pressable, Text, View } from "react-native";
import { useCurrentServer } from "../../store/currentServer";
import { useAppTheme } from "../../constants/tokens";
import { browserRequest, type BrowserResource } from "../../services/browser";
import { getDefaultBrowserId } from "../../services/browserDefault";

// Selected anew for each launch. Never carries a profile across server switches.
// The current server's default browser is preselected when it is open.
export function BrowserAttachmentPicker({ value, onChange }: {
  value?: string;
  onChange(value?: string, automatic?: boolean): void;
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
      void Promise.all([
        browserRequest(server, { action: "list" }, controller.signal),
        getDefaultBrowserId(server.id),
      ]).then(([result, remembered]) => {
        if (controller.signal.aborted || !isCurrentServer(server.id)) return;
        const usable = (result.resources || []).filter((r) => r.allow_agents && r.state === "running");
        setResources(usable);
        const preferred = usable.find((r) => r.id === remembered);
        if (preferred) onChange(preferred.id, true);
      }).catch(() => { if (!controller.signal.aborted) setError("Browsers unavailable. You can start without one."); });
    }
    return () => controller.abort();
    // onChange is a fresh closure each render; reload only when the server changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentServer, isCurrentServer]);
  if (!error && resources.length === 0) return null;
  return <View style={{ gap: 8 }}>
    <Text style={{ color: colors.textSecondary }}>Browser</Text>
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
      {[{ id: "", name: "None" }, ...resources].map((resource) => <Pressable
        key={resource.id} accessibilityRole="radio" accessibilityState={{ selected: (value || "") === resource.id }}
        onPress={() => onChange(resource.id || undefined)}
        style={{ minHeight: 44, justifyContent: "center", paddingHorizontal: 14, borderRadius: 12, backgroundColor: (value || "") === resource.id ? colors.surfaceSubtle : colors.bgSurface }}>
        <Text style={{ color: colors.textPrimary }}>{(value || "") === resource.id ? "✓ " : ""}{resource.name}</Text>
      </Pressable>)}
    </View>
    <Text style={{ color: colors.textSecondary, fontSize: 12 }}>{error || "Codex and Claude sessions can use a browser."}</Text>
  </View>;
}
