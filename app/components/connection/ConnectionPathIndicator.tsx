import React from "react";
import { StatusPill } from "../ui/StatusPill";

export function ConnectionPathIndicator({ connected, issue }: {
  connected: boolean;
  issue?: string | null;
}) {
  if (connected) return null;
  return <StatusPill label={issue || "Offline"} tone="warning" />;
}
