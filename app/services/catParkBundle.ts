import AsyncStorage from "@react-native-async-storage/async-storage";
import * as Crypto from "expo-crypto";
import { Directory, File, Paths } from "expo-file-system";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import { CAT_ARCHIVE, CAT_BUNDLE_PURPOSE, CAT_INDEX, parseCatManifest, staleVersions } from "./catPark";
import { runCatSync, type CatSyncDeps, type CatSyncProgress } from "./catParkSync";
import { bytesToHex } from "./protocolCrypto";
import { requestFailure, signedFetchBytes } from "./signedRequest";
import type { StoredServer } from "./storage";

/** The cat's page on disk, ready for a WebView to open. */
export interface CatBundle {
  version: string;
  dirUri: string;
  indexUri: string;
}

const KEEP_VERSIONS = 2;
const lastKey = (serverId: string) => `mewla.cat.bundle.last.v1:${serverId}`;
const orderKey = "mewla.cat.bundle.order.v1";

function versionDirectory(version: string): Directory {
  return new Directory(Paths.document, "cat", version);
}

function bundleOf(version: string): CatBundle {
  const directory = versionDirectory(version);
  const dirUri = directory.uri.endsWith("/") ? directory.uri : `${directory.uri}/`;
  return { version, dirUri, indexUri: `${dirUri}${CAT_INDEX}` };
}

function sizesIn(directory: Directory, prefix = "", out = new Map<string, number>()): Map<string, number> {
  if (!directory.exists) return out;
  for (const entry of directory.list()) {
    if (entry instanceof Directory) sizesIn(entry, `${prefix}${entry.name}/`, out);
    else out.set(`${prefix}${entry.name}`, entry.size ?? -1);
  }
  return out;
}

/**
 * The bundle this phone already holds for a computer, so the house opens at
 * once and offline. Null before the first sync (or if the files were cleared).
 */
export async function cachedCatBundle(serverId: string): Promise<CatBundle | null> {
  const version = await AsyncStorage.getItem(lastKey(serverId)).catch(() => null);
  if (!version) return null;
  const bundle = bundleOf(version);
  return new File(bundle.indexUri).exists ? bundle : null;
}

const MANIFEST_TIMEOUT_MS = 30_000;
const FILE_TIMEOUT_MS = 30_000;
const ARCHIVE_TIMEOUT_MS = 90_000;
/** However slow the link, the first open ends here, with a retry. */
const SYNC_DEADLINE_MS = 120_000;

async function fetchBytes(server: StoredServer, pathname: string, timeoutMs: number): Promise<{ response: Response; bytes: Uint8Array | null }> {
  const identity = await getOrCreateLocalDeviceIdentity();
  return signedFetchBytes(server, pathname, CAT_BUNDLE_PURPOSE, identity, timeoutMs);
}

function depsFor(server: StoredServer): CatSyncDeps {
  return {
    async manifest() {
      const { response, bytes } = await fetchBytes(server, "/cat/manifest.json", MANIFEST_TIMEOUT_MS);
      if (!bytes) throw await requestFailure(server, response);
      const manifest = parseCatManifest(JSON.parse(new TextDecoder().decode(bytes)));
      if (!manifest) throw new Error(`${server.name} sent a cat bundle this app cannot use.`);
      return manifest;
    },
    async archive() {
      const { response, bytes } = await fetchBytes(server, `/cat/${CAT_ARCHIVE}`, ARCHIVE_TIMEOUT_MS);
      if (response.status === 404) return null;
      if (!bytes) throw await requestFailure(server, response);
      return bytes;
    },
    async file(file) {
      const { response, bytes } = await fetchBytes(server, `/cat/${file.path}`, FILE_TIMEOUT_MS);
      if (!bytes) throw await requestFailure(server, response);
      return bytes;
    },
    sha256Hex: async (bytes) => bytesToHex(new Uint8Array(await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, bytes as Uint8Array<ArrayBuffer>))),
    present(manifest) {
      const directory = versionDirectory(manifest.version);
      directory.create({ intermediates: true, idempotent: true });
      return sizesIn(directory);
    },
    write(manifest, file, bytes) {
      const target = new File(versionDirectory(manifest.version), ...file.path.split("/"));
      target.parentDirectory.create({ intermediates: true, idempotent: true });
      if (target.exists) target.delete();
      target.create({ intermediates: true });
      target.write(bytes);
    },
  };
}

const running = new Map<string, { done: Promise<CatBundle>; listeners: Set<(progress: CatSyncProgress) => void> }>();

/**
 * Brings the phone's copy of the computer's bundle up to date: the manifest
 * names every file with its digest, what is missing arrives as one archive (or
 * file by file from a computer that has none), and each file is checked against
 * its digest before it is written. The version is remembered only once every
 * file is in place. A sync already running for this computer is joined, not
 * repeated, and none runs past its deadline.
 */
export function syncCatBundle(server: StoredServer, onProgress?: (progress: CatSyncProgress) => void): Promise<CatBundle> {
  const current = running.get(server.id);
  if (current) {
    if (onProgress) current.listeners.add(onProgress);
    return current.done;
  }
  const listeners = new Set<(progress: CatSyncProgress) => void>();
  if (onProgress) listeners.add(onProgress);
  const done = (async () => {
    const manifest = await runCatSync(depsFor(server), {
      deadlineMs: SYNC_DEADLINE_MS,
      onProgress: (progress) => listeners.forEach((listener) => listener(progress)),
    });
    await AsyncStorage.setItem(lastKey(server.id), manifest.version);
    await prune(manifest.version);
    return bundleOf(manifest.version);
  })().finally(() => running.delete(server.id));
  running.set(server.id, { done, listeners });
  return done;
}

async function prune(inUse: string): Promise<void> {
  try {
    const order = [inUse, ...(JSON.parse((await AsyncStorage.getItem(orderKey)) ?? "[]") as string[]).filter((v) => v !== inUse)].slice(0, KEEP_VERSIONS + 1);
    await AsyncStorage.setItem(orderKey, JSON.stringify(order));
    const root = new Directory(Paths.document, "cat");
    const names = root.list().filter((entry): entry is Directory => entry instanceof Directory).map((entry) => entry.name);
    for (const stale of staleVersions(names, inUse, order, KEEP_VERSIONS)) {
      new Directory(root, stale).delete();
    }
  } catch {
    // Housekeeping never blocks the cat.
  }
}
