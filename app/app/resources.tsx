import React from "react";
import { useCurrentServer } from "../store/currentServer";
import { ResourcesView } from "../components/resources/ResourcesView";
import { useResourceTelemetry } from "../components/resources/useResourceTelemetry";

export default function ResourcesScreen() {
  const { currentServer } = useCurrentServer();
  const { telemetry, loading, error, connected, retry } = useResourceTelemetry();
  return (
    <ResourcesView
      telemetry={telemetry}
      loading={loading}
      error={error}
      connected={connected}
      hasServer={currentServer !== null}
      onRetry={retry}
    />
  );
}
