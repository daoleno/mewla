import { afterEach, describe, expect, test } from "bun:test";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import type { StoredServer } from "../storedServerContract";
import { changePhone, getPhoneStatus } from "./api";
import { isPhoneLink, PHONE_CLOSE, PHONE_LINK_BACKOFF_MS, PHONE_PROTOCOL_VERSION, phoneDeviceURL, phoneLinkTarget, sameTarget } from "./link";

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

  test("protocol 1, the daemon's close codes and the 1-2-4-8-15 s backoff", () => {
    expect(PHONE_PROTOCOL_VERSION).toBe(1);
    expect(PHONE_CLOSE).toEqual({ replaced: 4001, unsupported: 4002, protocol: 4003 });
    expect([...PHONE_LINK_BACKOFF_MS]).toEqual([1_000, 2_000, 4_000, 8_000, 15_000]);
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

  test("GET /phone is signed for mewla-phone:GET:/phone", async () => {
    answering({ enabled: true, connected: false });
    expect(await getPhoneStatus(server)).toEqual({ enabled: true, connected: false });
    expect(seen[0]).toMatchObject({ url: "http://desk.local:9877/phone", method: "GET" });
    expect(seen[0].auth).toStartWith(`Device v1:`);
    expect(seen[0].auth).toContain(`:${DAEMON}:`);
  });

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
