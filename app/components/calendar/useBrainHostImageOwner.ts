import { useMemo } from "react";
import { useBrain } from "../../store/brain";
import { useCurrentServer } from "../../store/currentServer";
import { wsClient } from "../../services/websocket";
import type { ImageOwner } from "../../services/imageSource";
import {
  bindSessionFileRequestToGeneration,
  buildSessionFileBinarySource,
} from "../../services/sessionFilePreview";

/**
 * Images in a Calendar run's result load the way the same result does in
 * Brain's conversation: through Brain's host Session on this server, so a
 * draft's cover reads the same in both places.
 */
export function useBrainHostImageOwner(serverId: string | undefined): ImageOwner | null {
  const { state } = useBrain();
  const { currentServer } = useCurrentServer();
  const host = serverId ? state.byServer[serverId]?.host_worker : null;
  const daemonId = currentServer?.id === serverId ? currentServer?.daemonId ?? "" : "";
  const workerId = host?.id;
  const processId = host?.process_id;
  const startedAt = host?.started_at;
  return useMemo<ImageOwner | null>(() => {
    if (!serverId || !workerId || !processId || !startedAt) return null;
    return {
      key: JSON.stringify(["brain-host", serverId, daemonId, workerId, processId, startedAt]),
      async resolve(path, signal) {
        const request = { workerId, processId, startedAt, path };
        const metadata = await wsClient.getSessionFileMetadata(serverId, request);
        if (signal.aborted) throw new Error("Image request cancelled.");
        if (metadata.kind !== "image" || metadata.tooLarge) {
          throw new Error("This image exceeds the preview limit or is unsupported.");
        }
        const source = await buildSessionFileBinarySource(
          serverId,
          daemonId,
          bindSessionFileRequestToGeneration(request, metadata),
        );
        if (signal.aborted) throw new Error("Image request cancelled.");
        return { ...source, mimeType: metadata.contentType };
      },
    };
  }, [daemonId, processId, serverId, startedAt, workerId]);
}
