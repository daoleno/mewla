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

describe("Pairing V2", () => {
  test("accepts daemon-signed Link route, pin, admission, and stable candidates", () => {
    const seed = new Uint8Array(32).fill(7);
    const keyPair = nacl.sign.keyPair.fromSeed(seed);
    const payload = {
      v: 2,
      d: "1".repeat(64),
      k: bytesToHex(keyPair.publicKey),
      e: "2".repeat(64),
      r: "3".repeat(32),
      p: "4".repeat(64),
      c: [
        {
          n: "region-a",
          a: "wss://55555555555555555555555555555555.a.link.test/ws",
          s: "wss://33333333333333333333333333333333.a.link.test/ws",
        },
        {
          n: "region-b",
          s: "wss://33333333333333333333333333333333.b.link.test/ws",
        },
      ],
      x: Date.now() + 60_000,
      z: "",
    };
    const binding = new TextEncoder().encode(
      [
        "2",
        payload.d,
        payload.k,
        payload.e,
        payload.r,
        payload.p,
        payload.x.toString(),
        "region-a",
        payload.c[0].a,
        payload.c[0].s,
        "region-b",
        "",
        payload.c[1].s,
      ].join("\n"),
    );
    const domain = new TextEncoder().encode("mewla-link-pairing-v2\u0000");
    const signed = new Uint8Array(domain.length + binding.length);
    signed.set(domain);
    signed.set(binding, domain.length);
    payload.z = bytesToHex(nacl.sign.detached(signed, keyPair.secretKey));

    const encoded = Buffer.from(JSON.stringify(payload)).toString("base64url");
    const link = `mewla://settings?v=2&p=${encoded}`;
    expect(parseConnectLink(`other://settings?v=2&p=${encoded}`)).toBeNull();
    expect(parseConnectLink(link)).toEqual({
      url: payload.c[0].a!,
      daemonId: payload.d,
      daemonPublicKey: payload.k,
      enrollmentToken: payload.e,
      link: {
        kind: "link",
        routeId: payload.r,
        transportPin: payload.p,
        candidates: [
          {
            name: "region-a",
            admissionUrl: payload.c[0].a!,
            url: payload.c[0].s,
          },
          {
            name: "region-b",
            admissionUrl: undefined,
            url: payload.c[1].s,
          },
        ],
      },
    });

    payload.p = "6".repeat(64);
    const tampered = `mewla://settings?v=2&p=${Buffer.from(
      JSON.stringify(payload),
    ).toString("base64url")}`;
    expect(parseConnectLink(tampered)).toBeNull();

    const oversized = `mewla://settings?v=2&p=${"A".repeat((64 << 10) + 1)}`;
    expect(parseConnectLink(oversized)).toBeNull();
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
