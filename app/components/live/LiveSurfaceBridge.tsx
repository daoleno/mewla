import { useEffect } from "react";

import { getMewlaLiveModule } from "../../modules/mewla-live/src";
import { widgetBaseUrl } from "../../services/liveSurface";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";

// One token request at a time: the daemon keeps a single token per device, so a second
// request in flight would replace the token the first one is about to hand over.
let provisioning = false;

/**
 * Headless. Keeps the Android live notification and home-screen widgets bound to the one
 * current server: it applies live_state from the socket (pushes reach the native service
 * directly), catches up on connect, and gives the widgets their own revocable token.
 * iOS has no module yet and does nothing here.
 */
export function LiveSurfaceBridge() {
  const { currentServer, hydrated } = useCurrentServer();
  const serverId = currentServer?.id ?? "";
  const baseUrl = currentServer ? widgetBaseUrl(currentServer) : null;

  // Server switch (or none): the native side drops the old server's surface and widget cache.
  // Not before the stored server has loaded: "no server yet" is not "no server", and treating
  // it so would clear the widget credentials on every cold start.
  useEffect(() => {
    if (!hydrated) return;
    getMewlaLiveModule()?.setCurrentServer(serverId);
  }, [hydrated, serverId]);

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
      if (provisioning || live.widgetState() !== "none") return;
      if (!baseUrl) {
        // Mewla Link has no address a widget can fetch, and no token to give it: it shows the
        // states this app and the pushes apply.
        live.configureWidgetPushed();
        return;
      }
      provisioning = true;
      try {
        const { token } = await wsClient.issueWidgetToken(serverId);
        if (token && !cancelled) live.configureWidget(baseUrl, token);
      } catch (error) {
        console.log("Widget token could not be issued:", error);
      } finally {
        provisioning = false;
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
