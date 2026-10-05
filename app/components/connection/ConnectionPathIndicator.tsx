import React from "react";
import { StatusPill } from "../ui/StatusPill";

export function ConnectionPathIndicator({ connected, latencyMs, transportKind, issue }: {
  connected: boolean;
  latencyMs?: number;
  transportKind?: "manual" | "link";
  issue?: string | null;
}) {
  if (!connected) return <StatusPill label={issue || "Offline"} tone="warning" />;
  const path = transportKind === "link" ? "via relay" : "direct";
  const latency = typeof latencyMs === "number" ? ` · ${latencyMs} ms` : "";
  return <StatusPill label={`Connected · ${path}${latency}`} tone="success" live />;
}
