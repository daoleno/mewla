import { describe, expect, mock, test } from "bun:test";
import nacl from "tweetnacl";

mock.module("react-native", () => ({ Platform: { OS: "ios" } }));
mock.module("expo-crypto", () => ({
  getRandomBytes: (length: number) => new Uint8Array(length),
  randomUUID: () => "00000000-0000-4000-8000-000000000000",
}));
mock.module("expo-device", () => ({ deviceName: "Test", modelName: "Test" }));
mock.module("expo-secure-store", () => ({
  getItemAsync: async () => null,
  setItemAsync: async () => undefined,
}));
mock.module("@react-native-async-storage/async-storage", () => ({
  default: {
    getItem: async () => null,
    setItem: async () => undefined,
  },
}));

const { bytesToHex } = await import("./protocolCrypto");
const { parseConnectLink } = await import("./connection");
const { buildLinkPairingTestLink } = await import("./linkPairingTestLink");

describe("Pairing links from the app's own builder", () => {
  test("accepts a daemon-signed Link route, pin and admission, and rejects tampering", () => {
    const keyPair = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(7));
    const input = {
      secretKey: keyPair.secretKey,
      publicKey: keyPair.publicKey,
      enrollmentToken: "2".repeat(64),
      routeId: "3".repeat(32),
      transportPin: "4".repeat(64),
      label: "quiet-otter-3f9a",
      domain: "a.link.test",
      name: "region-a",
      admissionAlias: "55555555555555555555555555555555",
    };
    const link = buildLinkPairingTestLink(input);
    expect(parseConnectLink(`other://${link.slice("mewla://".length)}`)).toBeNull();
    expect(parseConnectLink(link)).toEqual({
      url: "wss://55555555555555555555555555555555.a.link.test/ws",
      daemonId: undefined,
      daemonPublicKey: bytesToHex(keyPair.publicKey),
      enrollmentToken: input.enrollmentToken,
      link: {
        kind: "link",
        routeId: input.routeId,
        transportPin: input.transportPin,
        candidates: [
          {
            name: "region-a",
            admissionUrl: "wss://55555555555555555555555555555555.a.link.test/ws",
            url: "wss://quiet-otter-3f9a.a.link.test/ws",
          },
        ],
      },
    });

    expect(
      parseConnectLink(link.replace(/.$/, (c) => (c === "A" ? "B" : "A"))),
    ).toBeNull();
    expect(parseConnectLink(buildLinkPairingTestLink({ ...input, expiresAtMs: Date.now() - 1 }))).toBeNull();
    expect(parseConnectLink(`mewla://settings?v=3&p=${"A".repeat((64 << 10) + 1)}`)).toBeNull();
  });

  test("rejects the retired version 2 JSON link", () => {
    const payload = {
      v: 2,
      d: "1".repeat(64),
      k: "7".repeat(64),
      e: "2".repeat(64),
      r: "3".repeat(32),
      p: "4".repeat(64),
      c: [{ n: "a", a: "wss://55555555555555555555555555555555.a.link.test/ws", s: "wss://33333333333333333333333333333333.a.link.test/ws" }],
      x: Date.now() + 60_000,
      z: "0".repeat(128),
    };
    const encoded = Buffer.from(JSON.stringify(payload)).toString("base64url");
    expect(parseConnectLink(`mewla://settings?v=2&p=${encoded}`)).toBeNull();
  });
});

// Shared with daemon/link/pairing_compact_test.go: the Go encoder and this
// decoder must agree on the version 3 layout and its signature binding.
const COMPACT_GOLDEN =
  "AN43KVROR7TFJ6KAPCYRF2FJROTZAH4FHLTJLPWX4DRZCC5NASLGIIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCGMZTGMZTGMZTGMZTGMZTGMZTGNCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIA53FTB5QABK2LOJ5ZDR7UD6VSZJ3GNTCZ2SAFQAX6EXGTCUOC62XJM3WVZS7AMVPXIYOOBR7W373DWAZR4Z5XNYQSIWIZL2J6YTNTDXCT5323VAWEDROVUWK5BNN52HIZLSFUZWMOLBAECG2YLJNYFG2LTNMV3WYYJOMFUQDOYBKVKVKVKVKVKVKVKVKVKVKVKVKU";

describe("Pairing V3", () => {
  test("reads the daemon's compact Link pairing payload", () => {
    const seed = Uint8Array.from({ length: 32 }, (_, index) => index + 1);
    const keyPair = nacl.sign.keyPair.fromSeed(seed);
    const parsed = parseConnectLink(`mewla://settings?v=3&p=${COMPACT_GOLDEN}`);
    expect(parsed).toEqual({
      url: `wss://${"55".repeat(16)}.m.mewla.ai/ws`,
      daemonId: undefined,
      daemonPublicKey: bytesToHex(keyPair.publicKey),
      enrollmentToken: "22".repeat(32),
      link: {
        kind: "link",
        routeId: "33".repeat(16),
        transportPin: "44".repeat(32),
        candidates: [
          {
            name: "main",
            admissionUrl: `wss://${"55".repeat(16)}.m.mewla.ai/ws`,
            url: "wss://quiet-otter-3f9a.m.mewla.ai/ws",
          },
        ],
      },
    });
    expect(
      parseConnectLink(
        `https://quiet-otter-3f9a.m.mewla.ai/#pair=${encodeURIComponent(`mewla://settings?v=3&p=${COMPACT_GOLDEN}`)}`,
      )?.enrollmentToken,
    ).toBe("22".repeat(32));
  });

  test("rejects any changed character, other encodings and versions", () => {
    const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    for (let index = 0; index < COMPACT_GOLDEN.length; index++) {
      const current = COMPACT_GOLDEN[index];
      const replacement = alphabet[(alphabet.indexOf(current) + 1) % alphabet.length];
      const changed = COMPACT_GOLDEN.slice(0, index) + replacement + COMPACT_GOLDEN.slice(index + 1);
      expect(parseConnectLink(`mewla://settings?v=3&p=${changed}`)).toBeNull();
    }
    expect(parseConnectLink(`mewla://settings?v=3&p=${COMPACT_GOLDEN.toLowerCase()}`)).toBeNull();
    expect(parseConnectLink(`mewla://settings?v=3&p=${COMPACT_GOLDEN.slice(0, -2)}`)).toBeNull();
    expect(parseConnectLink(`mewla://settings?v=3&p=${COMPACT_GOLDEN}AAAA`)).toBeNull();
    expect(parseConnectLink(`mewla://settings?v=2&p=${COMPACT_GOLDEN}`)).toBeNull();
    expect(parseConnectLink(`mewla://settings?p=${COMPACT_GOLDEN}`)).toBeNull();
  });
});

test("QR and paste accept HTTPS fragment links as well as mewla links", () => {
  const query = `u=${encodeURIComponent("wss://mewla.example/ws")}&k=${"a".repeat(64)}&t=${"b".repeat(64)}`;
  const current = `mewla://settings?${query}`;
  const expected = parseConnectLink(current);
  expect(expected).not.toBeNull();
  expect(parseConnectLink(`https://mewla.example/#pair=${encodeURIComponent(current)}`)).toEqual(expected);
  expect(parseConnectLink(`http://untrusted.example/#pair=${encodeURIComponent(current)}`)).toBeNull();
  expect(parseConnectLink(`other://settings?${query}`)).toBeNull();
  expect(parseConnectLink(`https://mewla.example/#pair=${encodeURIComponent(`other://settings?${query}`)}`)).toBeNull();
  expect(parseConnectLink("https://mewla.example/")).toBeNull();
  expect(parseConnectLink("https://mewla.example/#pair=https%3A%2F%2Fevil.example")).toBeNull();
});

test("reads the compact link the daemon prints for a plain LAN or Tailscale address", () => {
  // Printed by `mewla pair` on a daemon with no Mewla Link: a version 1 payload
  // (address, daemon key, one-time token) in the `p` parameter, no `v`.
  const printed =
    "mewla://settings?p=AQAXd3M6Ly8xMDAuOTIuMTc0LjkwOjk4OTVHHP7dxlX0zaLK5et3b5-J2j8CAtWfBkaokDtuDeNf4XI0E2sWgLagbSggpg8GyjHp4XBIrZmAi12ffdjAhOoX";
  const parsed = parseConnectLink(printed);
  expect(parsed?.url).toBe("ws://100.92.174.90:9895/ws");
  expect(parsed?.daemonPublicKey).toHaveLength(64);
  expect(parsed?.enrollmentToken).toHaveLength(64);
  expect(parsed?.link).toBeUndefined();
});
