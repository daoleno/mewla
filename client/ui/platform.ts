import { appInfo, host, platform as device, Storage, UrlScheme } from "nsh";
import { inflateRawSync } from "zlib";
import type { KeyValueStore, LinkTunnels, Platform, SocketLike } from "../core/platform";

// The secure store is the host's (Keychain / Keystore). A browser has none: there the
// device key lives in the page's storage under the prefix the previous web app used.
const secureFromHost: KeyValueStore = {
  getItem: async (key) => ((await host.call("secure.get", { key })) as string | null) ?? null,
  setItem: async (key, value) => void (await host.call("secure.set", { key, value })),
  removeItem: async (key) => void (await host.call("secure.remove", { key })),
};
const WEB_SECURE_PREFIX = "mewla:secure:";
const secureInStorage: KeyValueStore = {
  getItem: (key) => Storage.getItem(WEB_SECURE_PREFIX + key),
  setItem: (key, value) => Storage.setItem(WEB_SECURE_PREFIX + key, value),
  removeItem: (key) => Storage.removeItem(WEB_SECURE_PREFIX + key),
};

const linkTunnels: LinkTunnels = {
  start: async (key, host_, port, pin, mode) => (await host.call("link.start", { key, host: host_, port, pin, mode })) as { port: number; rttMs: number },
  stop: async (key) => void (await host.call("link.stop", { key })),
  events: async (key) => ((await host.call("link.events", { key })) as string[] | null) ?? [],
};

export function nshPlatform(): Platform {
  const os = device.os === "ios" || device.os === "android" || device.os === "web" ? device.os : "desktop";
  const params = host.params as { deviceName?: string };
  return {
    os,
    appVersion: appInfo().version ?? undefined,
    deviceName: () => params.deviceName ?? (os === "web" ? "Mewla Web" : "Mewla mobile"),
    randomBytes: (length) => crypto.getRandomValues(new Uint8Array(length)),
    randomUUID: () => crypto.randomUUID(),
    sha256: async (data) => new Uint8Array(crypto.createHash("sha256").update(data).digest()),
    storage: Storage,
    secure: host.available() ? secureFromHost : secureInStorage,
    openSocket: (url, protocols) => {
      const socket = new WebSocket(url, protocols);
      // Compressed messages (core/frames.ts) come as binary: as bytes, not a Blob.
      socket.binaryType = "arraybuffer";
      return socket as unknown as SocketLike;
    },
    // A browser negotiates permessage-deflate itself; the apps inflate the daemon's compressed messages.
    inflateRaw: os === "web" ? undefined : (data) => inflateRawSync(data),
    fetch: (input, init) => fetch(input, init),
    // The phone hosts and Mewla for Mac carry Mewla Relay's pinned tunnels (client/macos/src/link.rs); a browser can't.
    linkTunnels: host.available() && (device.os === "ios" || device.os === "android" || device.os === "macos") ? linkTunnels : undefined,
    pageOrigin: os === "web" ? pageOrigin() : undefined,
  };
}

/** The page's origin on the web (its address, as NSH keeps it). */
function pageOrigin(): string | undefined {
  try {
    const origin = new URL(UrlScheme.url() ?? "").origin;
    return origin && origin !== "null" ? origin : undefined;
  } catch {
    return undefined;
  }
}
