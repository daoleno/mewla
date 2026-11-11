import { describe, expect, test } from "bun:test";
import nacl from "../vendor/nacl";
import { buildServerAssertionPayload } from "../deviceAuthContract";
import { bytesToHex } from "../protocolCrypto";
import type { StoredServer } from "../storedServerContract";
import { diagnoseConnectionIssue } from "./connectionIssue";
import { directKind, PATH_LABELS, pathMark, pathOfConnection, type TransportPathInfo } from "./connectionPath";

const lan: TransportPathInfo = { path: "lan", host: "192.168.1.20", rttMs: 12.4 };
const tailscale: TransportPathInfo = { path: "tailscale", host: "100.64.0.9", rttMs: 28 };
const relay: TransportPathInfo = { path: "relay", host: null, rttMs: 140 };

describe("path mark", () => {
  test("LAN names its address, Tailscale and the relay do not", () => {
    expect(pathMark(lan, true)).toEqual({ path: "lan", kind: "direct", label: "LAN", detail: "LAN · 192.168.1.20 · 12 ms" });
    expect(pathMark(tailscale, true)).toEqual({ path: "tailscale", kind: "direct", label: "Tailscale", detail: "Tailscale · 28 ms" });
    expect(pathMark(relay, true)).toEqual({ path: "relay", kind: "relay", label: "Relay", detail: "Relay · 140 ms" });
  });

  test("latency is left out when it was not measured", () => {
    expect(pathMark({ ...relay, rttMs: null }, true)?.detail).toBe("Relay");
    expect(pathMark({ ...lan, rttMs: 0 }, true)?.detail).toBe("LAN · 192.168.1.20");
  });

  test("nothing is said without a path or a live connection", () => {
    expect(pathMark(null, true)).toBeNull();
    expect(pathMark(lan, false)).toBeNull();
  });

  test("the path follows the address the connection dials", () => {
    const base = { id: "s", name: "Desk", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) };
    expect(pathOfConnection({ ...base, url: "ws://192.168.1.20:9876/ws" })).toEqual({ path: "lan", host: "192.168.1.20", rttMs: null });
    expect(pathOfConnection({ ...base, url: "ws://100.101.1.2:9876/ws" })?.path).toBe("tailscale");
    expect(pathOfConnection({ ...base, url: "wss://desk.example.com/ws" })).toBeNull();
    expect(pathOfConnection({ ...base, url: "wss://x.m.mewla.ai/ws", transportKind: "link" })?.path).toBe("relay");
    expect(directKind("192.168.001.1")).toBeNull();
    expect(directKind("172.20.0.1")).toBe("lan");
    expect(PATH_LABELS.loopback).toBe("Local");
  });
});

describe("connection issue", () => {
  const keys = nacl.sign.keyPair();
  const daemonId = "d".repeat(64);
  const server: StoredServer = { id: "s", name: "Desk", url: "wss://desk.example.com/ws", daemonId, daemonPublicKey: bytesToHex(keys.publicKey) };
  const proof = (purpose: string) => {
    const nonce = "0".repeat(32);
    const timestamp = "1";
    const signature = nacl.sign.detached(buildServerAssertionPayload(purpose, daemonId, timestamp, nonce), keys.secretKey);
    return JSON.stringify({ daemon_id: daemonId, daemon_public_key: bytesToHex(keys.publicKey), assertion_timestamp: timestamp, assertion_nonce: nonce, assertion_signature: bytesToHex(signature) });
  };
  const deps = (answer: (url: string) => Response | Promise<Response>) => ({ fetch: async (url: string) => answer(url), authorization: async () => "Device x" });

  test("a computer that forgot this phone asks to pair again", async () => {
    const found = await diagnoseConnectionIssue({ server }, deps((url) => (url.endsWith("/health") ? new Response(proof("mewla-health")) : new Response("", { status: 401 }))));
    expect(found.code).toBe("device_not_paired");
    expect(found.title).toBe("Device is not paired");
    expect(found.hint).toBe("Import a fresh pairing link from mewla on this machine to enroll this phone again.");
  });

  test("a proxy that cannot reach the computer says so", async () => {
    const found = await diagnoseConnectionIssue({ server }, deps(() => new Response("", { status: 502 })));
    expect(found).toMatchObject({ code: "proxy_error", title: "Proxy could not reach daemon", httpStatus: 502 });
  });

  test("another service at the address is not the computer", async () => {
    const found = await diagnoseConnectionIssue({ server }, deps(() => new Response("{}")));
    expect(found.code).toBe("wrong_daemon");
  });

  test("a refused connection and a missing route", async () => {
    expect((await diagnoseConnectionIssue({ server }, deps(() => Promise.reject(new Error("ECONNREFUSED"))))).title).toBe("Daemon refused connection");
    expect((await diagnoseConnectionIssue({ server }, deps(() => new Response("", { status: 404 })))).code).toBe("path_not_found");
  });

  test("everything answers but the socket: the proxy drops the upgrade", async () => {
    const found = await diagnoseConnectionIssue(
      { server, socketError: "closed (1006)" },
      deps((url) => new Response(proof(url.endsWith("/health") ? "mewla-health" : "mewla-probe"))),
    );
    expect(found.code).toBe("websocket_upgrade_failed");
    expect(found.detail).toContain("The phone's socket reported: closed (1006).");
  });
});
