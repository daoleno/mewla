import { codexAsset } from "../../core/session/sessionRequests";
import type { Transport } from "../../core/transport";

/**
 * Reply images read from the computer (`codex_asset`), by computer, folder and path:
 * a recycled row or a streaming reply draws an image again without asking again.
 * Another computer clears them. Offline nothing is asked or remembered: the read
 * throws, and the picture (createPicture) reads again once the computer is back.
 */
const assets = new Map<string, Promise<string | null>>();
let assetsFor = "";

/** The image at `path` (relative to `cwd`) on the computer as a data URL, or null when it is no image; it throws when it could not ask. */
export function chatImage(transport: Transport | null, serverId: string, cwd: string | undefined, path: string): Promise<string | null> {
  if (!transport || transport.state !== "connected") return Promise.reject(new Error("Daemon is not connected."));
  if (assetsFor !== serverId) {
    assets.clear();
    assetsFor = serverId;
  }
  const key = `${cwd ?? ""}|${path}`;
  let asset = assets.get(key);
  if (!asset) {
    // Only a picture is kept: a read that did not answer or "no picture" is asked again (by the picture's own retry, or a tap).
    asset = codexAsset(transport, path, cwd).then(
      (url) => {
        if (!url) assets.delete(key);
        return url;
      },
      (error) => {
        assets.delete(key);
        throw error;
      },
    );
    assets.set(key, asset);
  }
  return asset;
}
