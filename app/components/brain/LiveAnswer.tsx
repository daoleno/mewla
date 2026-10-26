import { useGlobalSearchParams, useRouter } from "expo-router";
import { useEffect, useRef } from "react";

import { liveAnswerRequestId, parseLiveAnswerParams } from "../../services/liveSurface";
import { wsClient } from "../../services/websocket";
import { useCurrentServer } from "../../store/currentServer";
import { useToast } from "../ui/Toast";

/**
 * Headless. A tap on a live notification or widget opens Brain with the question and
 * choice in the route; this sends that one answer. The daemon refuses a question that is
 * no longer open, and the same tap twice is the same request, so a re-delivered intent
 * cannot answer twice.
 */
export function LiveAnswer() {
  const router = useRouter();
  const toast = useToast();
  const { currentServer } = useCurrentServer();
  const params = useGlobalSearchParams<{ liveAnswer?: string; liveChoice?: string }>();
  const handled = useRef<Set<string>>(new Set());

  const target = parseLiveAnswerParams(params.liveAnswer, params.liveChoice);
  const serverId = currentServer?.id ?? "";
  const requestId = target ? liveAnswerRequestId(target) : "";

  useEffect(() => {
    if (!target || !serverId || handled.current.has(requestId)) return;
    if (!wsClient.isConnected(serverId)) return;
    handled.current.add(requestId);
    // Consume the route params first so a recreated screen does not see them again.
    router.setParams({ liveAnswer: undefined, liveChoice: undefined });
    wsClient.answerLive(serverId, { ...target, requestId }).then(
      () => undefined,
      (error: Error) =>
        toast.show({
          title: "Couldn't send that answer",
          detail: error.message,
          tone: "error",
        }),
    );
  }, [requestId, serverId, target, router, toast]);

  return null;
}
