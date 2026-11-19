import { afterEach, describe, expect, test } from "bun:test";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import type { StoredServer } from "../storedServerContract";
import { changePhone } from "./api";
import { isPhoneLink, phoneDeviceURL, phoneLinkTarget, sameTarget } from "./link";

const DAEMON = "ab".repeat(32);

describe("the link's computer", () => {
  test("the socket is /phone/device on the computer's own address, ws for http and wss for https", () => {
    expect(phoneDeviceURL("http://desk.local:9877")).toBe("ws://desk.local:9877/phone/device");
    expect(phoneDeviceURL("https://desk.example.com/ws?x=1#y")).toBe("wss://desk.example.com/phone/device");
    expect(phoneDeviceURL("ws://10.0.0.2:9877/ws")).toBe("ws://10.0.0.2:9877/phone/device");
    expect(phoneDeviceURL("wss://desk.example.com/ws")).toBe("wss://desk.example.com/phone/device");
    expect(phoneDeviceURL("")).toBeNull();
    expect(phoneDeviceURL("ftp://desk")).toBeNull();
    expect(phoneDeviceURL("not a url")).toBeNull();
  });

  test("the target is the current computer with its id; none without one", () => {
    expect(phoneLinkTarget({ id: "server_1", url: "http://desk.local:9877", daemonId: DAEMON.toUpperCase() })).toEqual({
      serverId: "server_1",
      url: "ws://desk.local:9877/phone/device",
      daemonId: DAEMON,
    });
    expect(phoneLinkTarget(null)).toBeNull();
    expect(phoneLinkTarget({ id: "server_1", url: "http://desk.local:9877", daemonId: "" })).toBeNull();
    expect(phoneLinkTarget({ id: "server_1", url: "", daemonId: DAEMON })).toBeNull();
  });

  test("the same computer at the same address keeps its link; a switch or a move does not", () => {
    const a = phoneLinkTarget({ id: "a", url: "http://a:9877", daemonId: DAEMON });
    expect(sameTarget(a, phoneLinkTarget({ id: "a", url: "http://a:9877", daemonId: DAEMON }))).toBe(true);
    expect(sameTarget(a, phoneLinkTarget({ id: "b", url: "http://a:9877", daemonId: DAEMON }))).toBe(false);
    expect(sameTarget(a, phoneLinkTarget({ id: "a", url: "http://a2:9877", daemonId: DAEMON }))).toBe(false);
    expect(sameTarget(a, null)).toBe(false);
    expect(sameTarget(null, null)).toBe(true);
  });
});

describe("the link on Mewla Relay", () => {
  const PIN = "cd".repeat(32);
  const relayed = {
    id: "server_r",
    url: "wss://route.relay.example/ws",
    daemonId: DAEMON,
    transportKind: "link",
    transportPin: PIN.toUpperCase(),
    linkRouteId: "ef".repeat(16),
    transportCandidates: [
      { name: "Mewla Relay", kind: "link", url: "wss://route.relay.example/ws" },
      { name: "LAN", kind: "direct", url: "wss://192.168.1.20:9876/ws" },
    ],
  } as unknown as StoredServer;

  test("the link connects to the computer's own addresses with the pin, never through the app's tunnel", () => {
    const target = phoneLinkTarget(relayed, "ws://127.0.0.1:40001/ws");
    // The app's loopback tunnel stays as `url` for a host from before; the link itself uses `endpoints`.
    expect(target?.url).toBe("ws://127.0.0.1:40001/phone/device");
    expect(target?.pin).toBe(PIN);
    expect(target?.endpoints).toEqual(["wss://192.168.1.20:9876/phone/device", "wss://route.relay.example/phone/device"]);
    expect(target?.endpoints?.some((url) => url.includes("127.0.0.1"))).toBe(false);
  });

  test("a computer paired by address has no pin: the link uses its url as before", () => {
    const target = phoneLinkTarget({ id: "a", url: "http://desk.local:9877", daemonId: DAEMON });
    expect(target).toEqual({ serverId: "a", url: "ws://desk.local:9877/phone/device", daemonId: DAEMON });
  });
});

describe("mewla://phone", () => {
  test("the notification link opens the Phone page", () => {
    expect(isPhoneLink("mewla://phone")).toBe(true);
    expect(isPhoneLink("mewla://phone/")).toBe(true);
    expect(isPhoneLink(" mewla://phone?from=pairing ")).toBe(true);
  });

  test("other links are left alone", () => {
    expect(isPhoneLink("mewla://plugins?state=x")).toBe(false);
    expect(isPhoneLink("mewla://live/open")).toBe(false);
    expect(isPhoneLink("mewla://phones")).toBe(false);
    expect(isPhoneLink("/phone")).toBe(false);
  });
});

describe("the computer's Phone use", () => {
  const server = { id: "server_1", name: "Desk", url: "http://desk.local:9877", daemonId: DAEMON } as StoredServer;
  const seen: Array<{ url: string; method: string; auth: string; body?: string }> = [];
  afterEach(() => void seen.splice(0));

  function answering(body: object) {
    installMemoryPlatform({
      fetch: async (input, init) => {
        const headers = (init?.headers ?? {}) as Record<string, string>;
        seen.push({ url: String(input), method: init?.method ?? "GET", auth: headers.Authorization ?? "", body: init?.body as string | undefined });
        return new Response(JSON.stringify(body), { status: 200 });
      },
    });
  }

  test("the switch and Stop are one POST each, the signature over the body", async () => {
    answering({ enabled: true, connected: true });
    await changePhone(server, { enabled: true });
    await changePhone(server, { stop_agent: true });
    expect(seen.map((r) => [r.method, r.body])).toEqual([
      ["POST", '{"enabled":true}'],
      ["POST", '{"stop_agent":true}'],
    ]);
  });
});
