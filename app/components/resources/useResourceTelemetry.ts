import { useCallback, useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import { useFocusEffect } from "expo-router";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkerServerSummary } from "../../store/workers";
import { wsClient } from "../../services/websocket";
import {
  normalizeResourceTelemetry,
  type ResourceTelemetry,
} from "../../services/resourceTelemetry";

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
 * is focused and the app is in the foreground; daemon broadcasts are accepted
 * in the same window. State is bound to one server and dropped on a switch.
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
  const currentRef = useRef(current);
  currentRef.current = current;

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

      const schedule = () => {
        if (cancelled || !foreground) return;
        timer = setTimeout(poll, POLL_MS);
      };

      const poll = () => {
        timer = null;
        if (cancelled || !foreground || !isCurrentServer(targetServerId)) return;
        setInFlight(true);
        wsClient
          .getResourceTelemetry(targetServerId)
          .then((telemetry) => {
            if (cancelled || !isCurrentServer(targetServerId)) return;
            accept(targetServerId, telemetry);
          })
          .catch((error: unknown) => {
            if (cancelled || !isCurrentServer(targetServerId)) return;
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
            if (cancelled) return;
            setInFlight(false);
            schedule();
          });
      };

      const onBroadcast = (payload: any) => {
        if (payload?.serverId !== targetServerId || payload.request_id) return;
        const telemetry = normalizeResourceTelemetry(payload);
        if (telemetry && isCurrentServer(targetServerId)) accept(targetServerId, telemetry);
      };

      const appState = AppState.addEventListener("change", (next) => {
        const active = next === "active";
        if (active === foreground) return;
        foreground = active;
        if (timer) {
          clearTimeout(timer);
          timer = null;
        }
        if (active) poll();
      });

      wsClient.on("resource_telemetry", onBroadcast);
      if (foreground) poll();
      return () => {
        cancelled = true;
        if (timer) clearTimeout(timer);
        appState.remove();
        wsClient.off("resource_telemetry", onBroadcast);
        setInFlight(false);
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
