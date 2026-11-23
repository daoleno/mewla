import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { noteDaemonProves, noteProvedAt, resetDaemonProofsForTests } from "../daemonProof";
import { buildServerAssertionPayload } from "../deviceAuthContract";
import { buildUploadHeaders } from "../files/uploads";
import { bytesToHex } from "../protocolCrypto";
import { checkComputer } from "../settings/computer";
import type { StoredServer } from "../storedServerContract";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import nacl from "../vendor/nacl";
import { deviceAuthorization, signedJSON } from "./daemon";

// Security review 11-23, round 2 F4: signed HTTP, uploads and the Screen viewer to a computer held to proofs go only to
// an address where it proved itself a moment ago (its socket's proof, or a probe of its /auth-check over this app's
// challenge). A pairing at a host that is not the computer gets nothing of this app's, and none of its answers count.
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(41));
const impostorKeys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(42));
const DAEMON_ID = "d4".repeat(32);
const server: StoredServer = { id: "s1", name: "Desk", url: "wss://desk.example/ws", daemonId: DAEMON_ID, daemonPublicKey: bytesToHex(keys.publicKey) };

type Answer = "proof" | "impostor" | "older" | "unpaired";
let asked: string[];
let answer: Answer;

function probeAnswer(request: Request | string, init?: RequestInit): Response {
  const header = String((init?.headers as Record<string, string>)?.Authorization ?? "");
  const nonce = header.split(":")[4] ?? "";
  const timestamp = new Date().toISOString();
  const sign = (purpose: string, signer = keys, n = nonce) => bytesToHex(nacl.sign.detached(buildServerAssertionPayload(purpose, DAEMON_ID, timestamp, n), signer.secretKey));
  if (answer === "unpaired") return new Response("unknown device", { status: 401 });
  if (answer === "older") {
    const own = "0f".repeat(16);
    return Response.json({ assertion_timestamp: timestamp, assertion_nonce: own, assertion_signature: sign("mewla-probe", keys, own) });
  }
  const signer = answer === "impostor" ? impostorKeys : keys;
  return Response.json({ name: "Desk", proof: { purpose: "mewla-probe-challenge", timestamp, nonce, signature: sign("mewla-probe-challenge", signer) } });
}

beforeEach(() => {
  asked = [];
  answer = "proof";
  resetDaemonProofsForTests();
  installMemoryPlatform({
    fetch: async (input, init) => {
      const url = String(input);
      asked.push(`${init?.method ?? "GET"} ${url.replace(/\?.*$/, "")}`);
      if (new URL(url).pathname === "/auth-check") return probeAnswer(url, init);
      return Response.json({ devices: [] });
    },
  });
});
afterEach(() => resetDaemonProofsForTests());

describe("a computer held to proofs", () => {
  beforeEach(() => noteDaemonProves(server.daemonPublicKey));

  for (const [why, given, says] of [
    ["something else answers at the address (another key)", "impostor", "Desk did not prove it is the computer this app paired"],
    ["the computer answers the old way (a Mewla from before proofs)", "older", "Desk runs an older Mewla. Update Mewla on Desk."],
  ] as const) {
    test(`${why}: the signed request, the upload and the Screen viewer never reach it`, async () => {
      answer = given;
      await expect(signedJSON(server, "GET", "/devices", "mewla-device-admin:list:GET:/devices")).rejects.toThrow(says);
      await expect(buildUploadHeaders(server, { uri: "file:///a.png", name: "a.png", mimeType: "image/png", size: 1 }, "https://desk.example/upload")).rejects.toThrow(says);
      await expect(deviceAuthorization(server, "mewla-screen-view", { url: "wss://desk.example/screen/viewer?v=1" })).rejects.toThrow(says);
      // Only the probes went out.
      expect(asked.every((line) => line === "GET https://desk.example/auth-check")).toBe(true);
      expect(asked.length).toBe(3);
    });
  }

  test("a computer that no longer knows this device says so in its own words", async () => {
    answer = "unpaired";
    await expect(signedJSON(server, "GET", "/devices", "mewla-device-admin:list:GET:/devices")).rejects.toThrow("unknown device");
    expect(asked).toEqual(["GET https://desk.example/auth-check"]);
  });

  test("its proof over this app's challenge lets the request go, and holds for a while at that origin only", async () => {
    await Promise.all([signedJSON(server, "GET", "/devices", "a"), signedJSON(server, "GET", "/devices", "b")]);
    // One probe for both, then the requests.
    expect(asked).toEqual(["GET https://desk.example/auth-check", "GET https://desk.example/devices", "GET https://desk.example/devices"]);
    await deviceAuthorization(server, "mewla-screen-view", { url: "wss://desk.example/screen/viewer" });
    expect(asked.length).toBe(3);
    // Another address of the same computer proves itself there too.
    await deviceAuthorization(server, "mewla-screen-view", { url: "ws://192.168.1.20:9876/screen/viewer" });
    expect(asked.at(-1)).toBe("GET http://192.168.1.20:9876/auth-check");
  });

  test("a socket that proved itself at the address counts: no probe", async () => {
    noteProvedAt(server.daemonPublicKey, "wss://desk.example/ws");
    await signedJSON(server, "GET", "/devices", "a");
    expect(asked).toEqual(["GET https://desk.example/devices"]);
  });

  test("a failed probe is not remembered: the next request asks again", async () => {
    answer = "impostor";
    await expect(signedJSON(server, "GET", "/devices", "a")).rejects.toThrow("did not prove");
    answer = "proof";
    await signedJSON(server, "GET", "/devices", "a");
    expect(asked).toEqual(["GET https://desk.example/auth-check", "GET https://desk.example/auth-check", "GET https://desk.example/devices"]);
  });

  test("checkComputer checks the proof in its own answer: one request", async () => {
    expect(await checkComputer(server)).toEqual({ state: "online", name: "Desk" });
    expect(asked).toEqual(["GET https://desk.example/auth-check"]);
  });
});

test("a computer that never proved (older mewla, or not yet) is asked for nothing first", async () => {
  await signedJSON(server, "GET", "/devices", "a");
  expect(asked).toEqual(["GET https://desk.example/devices"]);
});
