import React from "react";
import { useRouter } from "expo-router";
import { useCurrentServer } from "../store/currentServer";
import { ResourcesView } from "../components/resources/ResourcesView";
import { useResourceTelemetry } from "../components/resources/useResourceTelemetry";

export default function ResourcesScreen() {
  const router = useRouter();
  const { currentServer } = useCurrentServer();
  const { telemetry, loading, error, connected, retry } = useResourceTelemetry();
  return (
    <ResourcesView
      telemetry={telemetry}
      loading={loading}
      error={error}
      connected={connected}
      hasServer={currentServer !== null}
      onOpenSettings={() => router.push("/settings")}
      onRetry={retry}
    />
  );
}
