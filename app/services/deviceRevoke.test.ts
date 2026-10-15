import { afterAll, expect, mock, test } from "bun:test";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import nacl from "tweetnacl";

const secureValues = new Map<string, string>();
mock.module("react-native", () => ({ Platform: { OS: "android" } }));
mock.module("expo-crypto", () => ({
  CryptoDigestAlgorithm: { SHA256: "SHA-256" },
  digestStringAsync: async (_algorithm: string, value: string) =>
    createHash("sha256").update(value).digest("hex"),
  getRandomBytes: (length: number) => crypto.getRandomValues(new Uint8Array(length)),
  randomUUID: () => crypto.randomUUID(),
}));
mock.module("expo-device", () => ({ deviceName: "Revoke phone", modelName: "Revoke phone" }));
mock.module("expo-secure-store", () => ({
  getItemAsync: async (key: string) => secureValues.get(key) || null,
  setItemAsync: async (key: string, value: string) => {
    secureValues.set(key, value);
  },
}));
mock.module("@react-native-async-storage/async-storage", () => ({
  default: { getItem: async () => null, setItem: async () => undefined },
}));

const { revokeThisDevice } = await import("./deviceRevoke");
const { buildSignaturePayload } = await import("./deviceAuthContract");
const { hexToBytes } = await import("./protocolCrypto");
const seed = new Uint8Array(32).fill(3);
const keys = nacl.sign.keyPair.fromSeed(seed);
const identity = {
  deviceId: "11111111-2222-4333-8444-555555555555",
  deviceName: "Revoke phone",
  publicKeyHex: Buffer.from(keys.publicKey).toString("hex"),
  seedHex: Buffer.from(seed).toString("hex"),
};
const loadIdentity = async () => identity;

const requests: Array<{ method?: string; url?: string; authorization?: string; body: string }> = [];
const daemon = createServer((request, response) => {
  let body = "";
  request.on("data", (chunk) => (body += chunk));
  request.on("end", () => {
    requests.push({
      method: request.method,
      url: request.url,
      authorization: request.headers.authorization,
      body,
    });
    response.writeHead(200, { "Content-Type": "application/json" }).end("{}");
  });
});
await new Promise<void>((resolve) => daemon.listen(0, "127.0.0.1", resolve));
afterAll(() => {
  daemon.close();
  mock.restore();
});

const server = {
  id: "s1",
  name: "Mac",
  url: `http://127.0.0.1:${(daemon.address() as AddressInfo).port}`,
  daemonId: "d".repeat(64),
  daemonPublicKey: "e".repeat(64),
};

test("removing a computer asks it to revoke this phone with the daemon's purpose", async () => {
  expect(await revokeThisDevice(server as never, loadIdentity)).toBe(true);
  expect(requests).toHaveLength(1);
  expect(requests[0].method).toBe("DELETE");
  expect(requests[0].url).toBe("/devices");
  expect(JSON.parse(requests[0].body)).toEqual({ device_id: identity.deviceId });
  // daemon/auth DeviceRevokePurpose binds the signature to sha256(device id).
  const [, deviceId, daemonId, timestamp, nonce, signature] = requests[0]
    .authorization!.replace(/^Device /, "")
    .split(":");
  expect([deviceId, daemonId]).toEqual([identity.deviceId, server.daemonId]);
  const purpose = `mewla-device-admin:revoke:DELETE:/devices:sha256=${createHash("sha256").update(identity.deviceId).digest("hex")}`;
  expect(
    nacl.sign.detached.verify(
      buildSignaturePayload(purpose, daemonId, deviceId, timestamp, nonce),
      hexToBytes(signature),
      hexToBytes(identity.publicKeyHex),
    ),
  ).toBe(true);
});

test("an unreachable computer never blocks removal", async () => {
  const closed = createServer();
  await new Promise<void>((resolve) => closed.listen(0, "127.0.0.1", resolve));
  const port = (closed.address() as AddressInfo).port;
  await new Promise<void>((resolve) => closed.close(() => resolve()));
  expect(
    await revokeThisDevice({ ...server, url: `http://127.0.0.1:${port}` } as never, loadIdentity),
  ).toBe(false);
});
