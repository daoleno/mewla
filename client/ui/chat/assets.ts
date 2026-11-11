import { codexAsset } from "../../core/session/sessionRequests";
import type { Transport } from "../../core/transport";

/**
 * Reply images read from the computer (`codex_asset`), by computer, folder and path:
 * a recycled row or a streaming reply draws an image again without asking again.
 * Another computer clears them. Offline nothing is asked or remembered, so the
 * image loads once the computer is back.
 */
const assets = new Map<string, Promise<string | null>>();
let assetsFor = "";

/** The image at `path` (relative to `cwd`) on the computer as a data URL, or null when it is no image. */
export function chatImage(transport: Transport | null, serverId: string, cwd: string | undefined, path: string): Promise<string | null> {
  if (!transport || transport.state !== "connected") return Promise.resolve(null);
  if (assetsFor !== serverId) {
    assets.clear();
    assetsFor = serverId;
  }
  const key = `${cwd ?? ""}|${path}`;
  let asset = assets.get(key);
  if (!asset) {
    asset = codexAsset(transport, path, cwd).catch((error) => {
      assets.delete(key);
      throw error;
    });
    assets.set(key, asset);
  }
  return asset;
}
