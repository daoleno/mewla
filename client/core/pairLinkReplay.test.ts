import { afterEach, describe, expect, test } from "bun:test";
import nacl from "./vendor/nacl";
import { buildServerAssertionPayload } from "./deviceAuthContract";
import { buildLinkPairingTestLink } from "./testing/linkPairingTestLink";
import { installMemoryPlatform } from "./testing/memoryPlatform";
import { alreadyPairedWith, pairingScopeCopy, pairingTarget, pairWith, readPairingLink } from "./pairFlow";
import { bytesToHex } from "./protocolCrypto";
import { resetDaemonProofsForTests } from "./daemonProof";
import { mergeStoredServer, type StoredServer } from "./storedServerContract";

// Security review of PR #27, finding 1 (11-23): a `mewla://` link anyone can make (the computer's id and key are on every
// QR code) named a computer this app had paired, at the attacker's host. The confirmation was skipped for a known
// computer whatever the address, and the attacker's host answered `/pair` with an answer recorded from the real
// computer (signed 2026-01-01): a second "Desk" at the attacker's host became current, unseen.
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(21));
const KEY = bytesToHex(keys.publicKey);
const DAEMON_ID = "d1".repeat(32);
const DESK = "wss://desk.example/ws";
const EVIL = "wss://evil.example/ws";

const owned: StoredServer[] = [{ id: "s1", name: "Desk", url: DESK, daemonId: DAEMON_ID, daemonPublicKey: KEY, transportKind: "manual" }];
const link = (url: string) => `mewla://pair?u=${encodeURIComponent(url)}&n=Desk&d=${DAEMON_ID}&k=${KEY}&t=${"7".repeat(64)}`;

/** The computer's answer to some earlier pairing, kept by whoever saw it. */
function recordedAnswer() {
  const timestamp = "2026-01-01T00:00:00.000Z";
  const nonce = "9".repeat(32);
  const signature = nacl.sign.detached(buildServerAssertionPayload("mewla-pair", DAEMON_ID, timestamp, nonce), keys.secretKey);
  return { daemon_id: DAEMON_ID, daemon_public_key: KEY, assertion_timestamp: timestamp, assertion_nonce: nonce, assertion_signature: bytesToHex(signature) };
}

/** The computer's answer to this app's challenge (daemon/server handlePair), under `purpose`. */
function challengedAnswer(challenge: string, purpose = "mewla-pair-challenge") {
  const timestamp = new Date().toISOString();
  const signature = nacl.sign.detached(buildServerAssertionPayload(purpose, DAEMON_ID, timestamp, challenge), keys.secretKey);
  return { ...recordedAnswer(), proof: { purpose, timestamp, nonce: challenge, signature: bytesToHex(signature) } };
}

const answerJSON = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

afterEach(() => {
  installMemoryPlatform();
  resetDaemonProofsForTests();
});

describe("a link to a computer this app has, at another address (review 11-23, finding 1)", () => {
  test("asks first, naming the host the pairing will talk to, and pairs only there", async () => {
    const asked: string[] = [];
    installMemoryPlatform({
      os: "desktop",
      fetch: async (input) => {
        asked.push(String(input));
        return new Response(JSON.stringify(recordedAnswer()), { status: 200, headers: { "content-type": "application/json" } });
      },
    });
    const payload = readPairingLink(link(EVIL))!;
    expect(payload).not.toBeNull();
    const target = await pairingTarget(payload);
    expect(target).toMatchObject({ url: EVIL, host: "evil.example", name: "Desk" });
    expect(alreadyPairedWith(owned, payload, target)).toBe(false);
    const copy = pairingScopeCopy("Mac", target);
    expect(copy.title).toBe("Pair this Mac?");
    expect(copy.message).toContain("access to evil.example (Desk),");

    // And a person who confirms is not fooled: the recorded answer signs no challenge of this app's, and is stale.
    await expect(pairWith(payload)).rejects.toThrow("Pairing target failed daemon identity proof.");
    expect(asked.every((url) => url.startsWith("https://evil.example/"))).toBe(true);
  });

  test("what the person confirmed is where it pairs: the address the confirmation named, as a second entry", async () => {
    installMemoryPlatform({ os: "desktop", fetch: async (_input, init) => answerJSON(challengedAnswer(JSON.parse(String(init?.body)).challenge)) });
    const payload = readPairingLink(link(EVIL))!;
    const input = await pairWith(payload);
    expect(input.url).toBe((await pairingTarget(payload)).url);
    expect(mergeStoredServer(input, owned, () => "s2").servers.map((server) => server.url)).toEqual([EVIL, DESK]);
  });

  test("the same computer's code at the address this app holds re-pairs without asking", async () => {
    installMemoryPlatform({ os: "android" });
    const payload = readPairingLink(link(DESK))!;
    expect(alreadyPairedWith(owned, payload, await pairingTarget(payload))).toBe(true);
    // Another daemon id with the same key is not this computer.
    expect(alreadyPairedWith(owned, { ...payload, daemonId: "e2".repeat(32) }, await pairingTarget(payload))).toBe(false);
  });

  test("a Mewla Relay code at the pairing's own address asks unless it pins the transport key this app holds", async () => {
    installMemoryPlatform({ os: "android", linkTunnels: {} as never });
    const relay = (pin: string, alias = "5".repeat(32)) =>
      readPairingLink(buildLinkPairingTestLink({
        secretKey: keys.secretKey, publicKey: keys.publicKey, enrollmentToken: "2".repeat(64), routeId: "3".repeat(32), transportPin: pin,
        label: "quiet-otter-3f9a", domain: "m.mewla.ai", admissionAlias: alias, name: "region-a",
        moreRelays: [{ name: "region-b", domain: "n.mewla.ai" }],
      }))!;
    const stable = "wss://quiet-otter-3f9a.m.mewla.ai/ws";
    const held: StoredServer = {
      id: "s1", name: "Desk", url: stable, daemonId: DAEMON_ID, daemonPublicKey: KEY, transportKind: "link", transportPin: "4".repeat(64), linkRouteId: "3".repeat(32),
      transportCandidates: [{ name: "region-a", kind: "link", url: stable }, { name: "region-b", kind: "link", url: "wss://quiet-otter-3f9a.n.mewla.ai/ws" }],
    };
    const same = relay("4".repeat(64));
    expect(await pairingTarget(same)).toMatchObject({ url: stable, transportPin: "4".repeat(64), host: "quiet-otter-3f9a.m.mewla.ai" });
    expect(alreadyPairedWith([held], same, await pairingTarget(same))).toBe(true);
    // The stored address with another pin: the pairing would keep that pin, so it is a new target.
    const otherPin = relay("6".repeat(64));
    expect(alreadyPairedWith([held], otherPin, await pairingTarget(otherPin))).toBe(false);
    // The computer paired by its hosted address (no pin yet), then its first Relay code: asked, with that address.
    const hosted: StoredServer = { id: "s1", name: "Desk", url: stable, daemonId: DAEMON_ID, daemonPublicKey: KEY, transportKind: "manual" };
    expect(alreadyPairedWith([hosted], same, await pairingTarget(same))).toBe(false);
    // A heard LAN address is no address this app holds for the computer.
    const lanOnly = { ...held, url: "wss://quiet-otter-3f9a.n.mewla.ai/ws", transportCandidates: [{ name: "LAN", kind: "direct" as const, url: stable }] };
    expect(alreadyPairedWith([lanOnly], same, await pairingTarget(same))).toBe(false);
  });

  test("the confirmation keeps the code's name to one short line after the host", async () => {
    installMemoryPlatform({ os: "android" });
    const named = (name: string) => `mewla://pair?u=${encodeURIComponent(EVIL)}&n=${encodeURIComponent(name)}&d=${DAEMON_ID}&k=${KEY}&t=${"7".repeat(64)}`;
    const target = await pairingTarget(readPairingLink(named("Desk\n\u0000at desk.example"))!);
    expect(pairingScopeCopy("phone", target).message).toContain("access to evil.example (Desk at desk.example),");
    const long = await pairingTarget(readPairingLink(named("D".repeat(80)))!);
    expect(long.name).toBe(`${"D".repeat(39)}…`);
    // Format characters are dropped: a right-to-left override cannot turn the line around (round 2 P1-2).
    const turned = await pairingTarget(readPairingLink(named("Desk\u202e\u200belpmaxe.live"))!);
    expect(turned.name).toBe("Deskelpmaxe.live");
  });
});

// Security review 11-23, PR B item 5: `/pair` signs a challenge of this app's. An older mewla, which does not, is
// answered the old way only while it has never proved, and only with an answer signed within minutes.
describe("the computer's answer to /pair", () => {
  const sent: { challenge?: string }[] = [];
  const pairDesk = async (answer: (challenge: string) => unknown) => {
    installMemoryPlatform({
      os: "android",
      fetch: async (_input, init) => {
        const body = JSON.parse(String(init?.body)) as { challenge: string };
        sent.push(body);
        return answerJSON(answer(body.challenge));
      },
    });
    return pairWith(readPairingLink(link(DESK))!);
  };
  const oldAnswer = (signedAt: Date) => {
    const timestamp = signedAt.toISOString();
    const signature = nacl.sign.detached(buildServerAssertionPayload("mewla-pair", DAEMON_ID, timestamp, "8".repeat(32)), keys.secretKey);
    return { ...recordedAnswer(), assertion_timestamp: timestamp, assertion_nonce: "8".repeat(32), assertion_signature: bytesToHex(signature) };
  };

  test("is the computer's when it signs this app's challenge, and the app's challenge is fresh each time", async () => {
    await pairDesk((challenge) => challengedAnswer(challenge));
    await pairDesk((challenge) => challengedAnswer(challenge));
    expect(sent.at(-1)!.challenge).toMatch(/^[0-9a-f]{32}$/);
    expect(sent.at(-1)!.challenge).not.toBe(sent.at(-2)!.challenge);
  });

  test("is refused when it answers another challenge, or under the /health purpose anyone can have for any challenge", async () => {
    await expect(pairDesk(() => challengedAnswer("f".repeat(32)))).rejects.toThrow("identity proof");
    await expect(pairDesk((challenge) => challengedAnswer(challenge, "mewla-health-challenge"))).rejects.toThrow("identity proof");
  });

  test("from an older mewla (no proof) is taken while fresh, until that computer has once proved", async () => {
    await expect(pairDesk(() => oldAnswer(new Date(Date.now() - 10 * 60_000)))).rejects.toThrow("identity proof");
    expect((await pairDesk(() => oldAnswer(new Date()))).url).toBe(DESK);
    // It updates and answers a challenge once: from then on an answer without a proof is not that computer.
    await pairDesk((challenge) => challengedAnswer(challenge));
    await expect(pairDesk(() => oldAnswer(new Date(Date.now() - 10 * 60_000)))).rejects.toThrow("identity proof");
  });

  // Brain 11-23 (no silent failures): pairing again with that computer after it went back to an older Mewla says why.
  test("from a computer that proved before and answers the old way now, names it and the fix", async () => {
    await pairDesk((challenge) => challengedAnswer(challenge));
    await expect(pairDesk(() => oldAnswer(new Date()))).rejects.toThrow("Desk runs an older Mewla. Update Mewla on Desk.");
    // A recorded answer (signed long ago) is no sign of an older Mewla: it is refused as before.
    await expect(pairDesk(() => recordedAnswer())).rejects.toThrow("Pairing target failed daemon identity proof.");
  });
});
