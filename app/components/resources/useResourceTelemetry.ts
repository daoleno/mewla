import { useCallback, useEffect, useState } from "react";
import { AppState } from "react-native";
import { useFocusEffect } from "expo-router";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkerServerSummary } from "../../store/workers";
import { wsClient } from "../../services/websocket";
import type { ResourceTelemetry } from "../../services/resourceTelemetry";

// The daemon samples every five seconds; history carries the charts.
const POLL_MS = 5000;

export interface ResourceTelemetryState {
  telemetry: ResourceTelemetry | null;
  loading: boolean;
  error: string | null;
  connected: boolean;
  retry(): void;
}

interface Snapshot {
  serverId: string;
  telemetry: ResourceTelemetry | null;
  error: string | null;
}

/**
 * Live machine telemetry for the current server. Polls only while the screen
 * is focused and the app is in the foreground. State is bound to one server
 * and dropped on a switch. Pending reads are cancelled when polling stops.
 */
export function useResourceTelemetry(): ResourceTelemetryState {
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { serverConnections } = useWorkerServerSummary();
  const serverId = currentServer?.id ?? null;
  const connection = serverId ? serverConnections[serverId] : undefined;
  const connected = connection === "connected";
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [inFlight, setInFlight] = useState(false);
  const [retryToken, setRetryToken] = useState(0);
  const current = snapshot?.serverId === serverId ? snapshot : null;

  useEffect(() => {
    setSnapshot((previous) => (previous && previous.serverId !== serverId ? null : previous));
  }, [serverId]);

  const accept = useCallback((targetServerId: string, telemetry: ResourceTelemetry) => {
    setSnapshot((previous) => {
      const base = previous?.serverId === targetServerId ? previous : null;
      if (base?.telemetry && base.telemetry.sampledAt > telemetry.sampledAt) return base;
      return {
        serverId: targetServerId,
        telemetry,
        error: null,
      };
    });
  }, []);

  useFocusEffect(
    useCallback(() => {
      if (!serverId || !connected) return undefined;
      const targetServerId = serverId;
      let cancelled = false;
      let timer: ReturnType<typeof setTimeout> | null = null;
      let foreground = AppState.currentState === "active";
      let request: AbortController | null = null;

      const schedule = () => {
        if (cancelled || !foreground) return;
        timer = setTimeout(poll, POLL_MS);
      };

      const poll = () => {
        timer = null;
        if (cancelled || !foreground || request || !isCurrentServer(targetServerId)) return;
        const controller = new AbortController();
        request = controller;
        setInFlight(true);
        wsClient
          .getResourceTelemetry(targetServerId, controller.signal)
          .then((telemetry) => {
            if (cancelled || controller.signal.aborted || !isCurrentServer(targetServerId)) return;
            accept(targetServerId, telemetry);
          })
          .catch((error: unknown) => {
            if (cancelled || controller.signal.aborted || !isCurrentServer(targetServerId)) return;
            const message = error instanceof Error ? error.message : "Resource telemetry failed.";
            setSnapshot((previous) => {
              const base = previous?.serverId === targetServerId ? previous : null;
              return {
                serverId: targetServerId,
                telemetry: base?.telemetry ?? null,
                error: message,
              };
            });
          })
          .finally(() => {
            if (cancelled || request !== controller) return;
            request = null;
            setInFlight(false);
            schedule();
          });
      };

      const stop = () => {
        if (timer) clearTimeout(timer);
        timer = null;
        request?.abort();
        request = null;
        setInFlight(false);
      };

      const appState = AppState.addEventListener("change", (next) => {
        const active = next === "active";
        if (active === foreground) return;
        foreground = active;
        if (active) poll();
        else stop();
      });

      if (foreground) poll();
      return () => {
        cancelled = true;
        stop();
        appState.remove();
      };
    }, [accept, connected, isCurrentServer, retryToken, serverId]),
  );

  const retry = useCallback(() => setRetryToken((token) => token + 1), []);

  return {
    telemetry: current?.telemetry ?? null,
    loading: !current?.telemetry && (inFlight || connection === "connecting"),
    error: current?.error ?? null,
    connected,
    retry,
  };
}
