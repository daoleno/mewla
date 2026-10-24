import AsyncStorage from "@react-native-async-storage/async-storage";
import * as Crypto from "expo-crypto";
import { Directory, File, Paths } from "expo-file-system";
import { getOrCreateLocalDeviceIdentity } from "./auth";
import {
  CAT_BUNDLE_PURPOSE,
  CAT_INDEX,
  filesToFetch,
  parseCatManifest,
  staleVersions,
  type CatManifest,
  type CatManifestFile,
} from "./catPark";
import { bytesToHex } from "./protocolCrypto";
import { requestFailure, signedFetch } from "./signedRequest";
import type { StoredServer } from "./storage";

/** The cat's page on disk, ready for a WebView to open. */
export interface CatBundle {
  version: string;
  dirUri: string;
  indexUri: string;
}

const FETCH_PARALLEL = 4;
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

async function fetchOne(server: StoredServer, directory: Directory, file: CatManifestFile): Promise<void> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const response = await signedFetch(server, "GET", `/cat/${file.path}`, CAT_BUNDLE_PURPOSE, undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  const bytes = new Uint8Array(await response.arrayBuffer());
  if (bytes.byteLength !== file.size) throw new Error(`The cat's ${file.path} arrived incomplete.`);
  const digest = bytesToHex(new Uint8Array(await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, bytes)));
  if (digest !== file.sha256) throw new Error(`The cat's ${file.path} did not match its digest.`);
  const target = new File(directory, ...file.path.split("/"));
  target.parentDirectory.create({ intermediates: true, idempotent: true });
  if (target.exists) target.delete();
  target.create({ intermediates: true });
  target.write(bytes);
}

async function fetchManifest(server: StoredServer): Promise<CatManifest> {
  const identity = await getOrCreateLocalDeviceIdentity();
  const response = await signedFetch(server, "GET", "/cat/manifest.json", CAT_BUNDLE_PURPOSE, undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  const manifest = parseCatManifest(await response.json());
  if (!manifest) throw new Error(`${server.name} sent a cat bundle this app cannot use.`);
  return manifest;
}

/**
 * Brings the phone's copy of the computer's bundle up to date: the manifest
 * names every file with its digest, only missing files are fetched (signed,
 * a few at a time) and each is checked against its digest before it is
 * written. The version is remembered only once every file is in place.
 */
export async function syncCatBundle(server: StoredServer): Promise<CatBundle> {
  const manifest = await fetchManifest(server);
  const directory = versionDirectory(manifest.version);
  directory.create({ intermediates: true, idempotent: true });
  const missing = filesToFetch(manifest, sizesIn(directory));
  let next = 0;
  const worker = async () => {
    while (next < missing.length) {
      const file = missing[next++];
      await fetchOne(server, directory, file);
    }
  };
  await Promise.all(Array.from({ length: Math.min(FETCH_PARALLEL, missing.length) }, worker));

  await AsyncStorage.setItem(lastKey(server.id), manifest.version);
  await prune(manifest.version);
  return bundleOf(manifest.version);
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
