import React, { useEffect } from "react";
import { CloudflarePanel } from "../../components/plugins/CloudflareViews";
import { PluginsPage, usePluginsFlow } from "../../components/plugins/PluginsFlow";

/** Cloudflare's cf CLI on the current server: install, sign in, sign out. */
export default function CloudflareScreen() {
  const flow = usePluginsFlow();
  const { cloudflareRequest } = flow;
  // Opening the page asks cf again, in case it was signed in or out in a shell.
  useEffect(() => { void cloudflareRequest("cloudflare_status", true); }, [cloudflareRequest]);
  return <PluginsPage title="Cloudflare">
    <CloudflarePanel
      status={flow.cloudflare}
      running={flow.cloudflareRunning}
      error={flow.cloudflareError}
      serverName={flow.serverName}
      onAction={(action) => void flow.cloudflareRequest(action)}
    />
  </PluginsPage>;
}
