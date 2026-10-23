import { useEffect } from "react";

import { getMewlaLiveModule } from "../../modules/mewla-live/src";
import { widgetBaseUrl } from "../../services/liveSurface";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";

/**
 * Headless. Keeps the Android live notification and home-screen widgets bound to the one
 * current server: it applies live_state from the socket (pushes reach the native service
 * directly), catches up on connect, and gives the widgets their own revocable token.
 * iOS has no module yet and does nothing here.
 */
export function LiveSurfaceBridge() {
  const { currentServer } = useCurrentServer();
  const serverId = currentServer?.id ?? "";
  const baseUrl = currentServer ? widgetBaseUrl(currentServer) : null;

  // Server switch (or none): the native side drops the old server's surface and widget cache.
  useEffect(() => {
    getMewlaLiveModule()?.setCurrentServer(serverId);
  }, [serverId]);

  useEffect(() => {
    const live = getMewlaLiveModule();
    if (!live || !serverId) return;
    let cancelled = false;

    const applyState = (state: unknown) => {
      if (!state || cancelled) return;
      try {
        live.apply(JSON.stringify(state));
      } catch (error) {
        console.log("Live state could not be applied:", error);
      }
    };

    const onLive = (payload: { serverId?: string; live?: unknown }) => {
      if (payload.serverId === serverId) applyState(payload.live);
    };
    wsClient.on("live_state", onLive);

    const catchUp = () => {
      if (!wsClient.isConnected(serverId)) return;
      void wsClient.getLiveState(serverId).then(applyState, () => undefined);
      void provisionWidget();
    };

    const provisionWidget = async () => {
      // Only a widget that was never provisioned gets a token. A revoked one stays revoked until
      // the server is switched or cleared; re-issuing here would undo the revocation.
      if (!baseUrl || live.widgetState() !== "none") return;
      try {
        const { token } = await wsClient.issueWidgetToken(serverId);
        if (token && !cancelled) live.configureWidget(baseUrl, token);
      } catch (error) {
        console.log("Widget token could not be issued:", error);
      }
    };

    const onConnected = (payload: { serverId?: string }) => {
      if (payload.serverId === serverId) catchUp();
    };
    wsClient.on("connected", onConnected);
    catchUp();

    return () => {
      cancelled = true;
      wsClient.off("live_state", onLive);
      wsClient.off("connected", onConnected);
    };
  }, [serverId, baseUrl]);

  return null;
}
