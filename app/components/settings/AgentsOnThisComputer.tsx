import React, { useCallback, useEffect, useRef, useState } from "react";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { ListRow, ListSection } from "../ui";
import { InkSwitch } from "../plugins/PluginConnectionViews";
import {
  changeAgentUse,
  getAgentUse,
  type AgentUseChange,
  type AgentUseFix,
  type AgentUseStatus,
} from "../../services/agentUse";
import type { StoredServer } from "../../services/storage";

const FIX_TITLES: Record<AgentUseFix, (name: string) => string> = {
  install_extension: (name) => `Open the extension's page in Chrome on ${name}`,
  open_extension: (name) => `Open the extension in Chrome on ${name}`,
  retry: () => "Try again",
  open_settings: (name) => `Open the permission's settings on ${name}`,
};

const POLL_MS = 5_000;

// AgentsOnThisComputer is the one switch that lets agents use the current
// computer the way the user does (their Chrome first), with one line on
// what is missing and the one tap that fixes it.
export function AgentsOnThisComputer({ server, connected }: { server: StoredServer | null; connected: boolean }) {
  const [status, setStatus] = useState<AgentUseStatus | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [hint, setHint] = useState<string | null>(null);
  const serverRef = useRef(server);
  serverRef.current = server;

  const reload = useCallback(async () => {
    if (!server || !connected) return;
    try {
      const next = await getAgentUse(server);
      if (serverRef.current?.id !== server.id) return;
      setStatus(next);
      setError(null);
    } catch (reason) {
      if (serverRef.current?.id !== server.id) return;
      setStatus(null);
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }, [server, connected]);

  useEffect(() => {
    // A different computer: nothing of the old one's status may show.
    setStatus(null);
    setHint(null);
    setError(null);
    void reload();
  }, [reload]);

  // Until the browser is ready the user is usually fixing something on the
  // computer (adding the extension), so look again every few seconds.
  const waiting = Boolean(status?.enabled && (status.browser.state !== "ready" || (status.computer && status.computer.state !== "ready" && status.computer.state !== "unsupported") || status.computer?.active));
  useEffect(() => {
    if (!waiting) return;
    const timer = setInterval(() => void reload(), POLL_MS);
    return () => clearInterval(timer);
  }, [waiting, reload]);

  if (!server) return null;

  const apply = async (key: string, change: AgentUseChange, afterHint?: string) => {
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setBusy(key);
    setError(null);
    try {
      const next = await changeAgentUse(server, change);
      if (serverRef.current?.id !== server.id) return;
      setStatus(next);
      setHint(afterHint ?? null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(null);
    }
  };

  const pasteKey = async () => {
    const text = (await Clipboard.getStringAsync()).trim();
    if (!text) {
      setError("Copy the extension's key first; the extension shows it when you open it.");
      return;
    }
    await apply("key", { key: text });
  };

  const browser = status?.browser;
  const computer = status?.computer;
  const enabled = status?.enabled ?? false;
  const fix = browser?.fix;
  const footer =
    error ??
    hint ??
    (enabled
      ? "Agents ask you before they send, pay, delete or change an account."
      : `Agents on ${server.name} can't use its browser.`);

  return (
    <ListSection title={`Agents on ${server.name}`} footer={footer}>
      <ListRow
        title="Agents can use this computer"
        subtitle={status ? undefined : connected ? "Checking…" : "Not connected"}
        icon="desktop"
        disabled={!connected || !status || busy !== null}
        accessibilityRole="switch"
        trailing={
          <InkSwitch
            value={enabled}
            disabled={!connected || !status || busy !== null}
            accessibilityLabel="Agents can use this computer"
            onValueChange={(value) => void apply("enabled", { enabled: value })}
          />
        }
      />
      {enabled && browser ? (
        <ListRow title="Your Chrome" subtitle={browser.summary} icon="browser" numberOfLines={3} />
      ) : null}
      {enabled && fix ? (
        <ListRow
          title={FIX_TITLES[fix](server.name)}
          icon={fix === "retry" ? "refresh" : "open-external"}
          loading={busy === "fix"}
          disabled={!connected || busy !== null}
          onPress={() =>
            void apply(
              "fix",
              { fix },
              fix === "install_extension"
                ? `Chrome on ${server.name} now shows the Playwright Extension. Click Add to Chrome there, or from Screen.`
                : fix === "open_extension"
                  ? `The extension opened in Chrome on ${server.name}. Mewla picks up its key by itself.`
                  : undefined,
            )
          }
        />
      ) : null}
      {enabled && computer ? (
        <ListRow title="This computer's screen" subtitle={computer.summary} icon="desktop" numberOfLines={3} />
      ) : null}
      {enabled && computer?.fix ? (
        <ListRow
          title={FIX_TITLES[computer.fix](server.name)}
          icon="open-external"
          loading={busy === "computer-fix"}
          disabled={!connected || busy !== null}
          onPress={() =>
            void apply(
              "computer-fix",
              { fix: computer.fix! },
              `Settings opened on ${server.name}. Turn on Mewla there, or from Screen.`,
            )
          }
        />
      ) : null}
      {computer?.active ? (
        <ListRow
          title={`Stop ${computer.active.name || "the agent"}`}
          subtitle="It is using the screen now"
          icon="stop"
          destructive
          loading={busy === "stop"}
          disabled={!connected || busy !== null}
          onPress={() => void apply("stop", { stop_agent: true })}
        />
      ) : null}
      {enabled && (browser?.state === "no_key" || browser?.key_pasted) ? (
        <ListRow
          title={browser?.key_pasted ? "Use the key from Chrome again" : "Paste the extension's key"}
          subtitle={browser?.key_pasted ? "A pasted key is in use" : "From this phone's clipboard"}
          icon="clipboard"
          loading={busy === "key"}
          disabled={!connected || busy !== null}
          onPress={() => void (browser?.key_pasted ? apply("key", { key: "" }) : pasteKey())}
        />
      ) : null}
    </ListSection>
  );
}
