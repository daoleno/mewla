import { normalizePublicKeyHex, verifyDaemonAssertion } from "./deviceAuthContract";
import { platform } from "./platform";
import { bytesToHex } from "./protocolCrypto";

/**
 * The computer proves it holds its key now, for this app: it signs a challenge the app chose (daemon/auth
 * ChallengePurpose), never only a nonce and time of its own, which anyone who recorded one answer could play back
 * (security review 11-23, finding 1). Each answer has its own purpose, so the unauthenticated `/health` answer for a
 * challenge never passes as the `/pair`, probe or connect answer for the same one.
 */
export const PAIR_CHALLENGE_PURPOSE = "mewla-pair-challenge";
export const PROBE_CHALLENGE_PURPOSE = "mewla-probe-challenge";
export const CONNECT_PROOF_PURPOSE = "mewla-connect-proof";
export type ChallengePurpose = typeof PAIR_CHALLENGE_PURPOSE | typeof PROBE_CHALLENGE_PURPOSE | typeof CONNECT_PROOF_PURPOSE;

/** A fresh challenge: 16 random bytes, hex. */
export function newChallenge(): string {
  return bytesToHex(platform().randomBytes(16));
}

/** Whether `proof` (the daemon's `{purpose, timestamp, nonce, signature}`) answers `challengeHex` under `purpose`. */
export async function proofAnswers(input: { purpose: ChallengePurpose; challengeHex: string; daemonId: string; daemonPublicKey: string; proof: unknown }): Promise<boolean> {
  if (!input.proof || typeof input.proof !== "object") return false;
  const proof = input.proof as Record<string, unknown>;
  const text = (value: unknown) => (typeof value === "string" ? value : "");
  const challenge = input.challengeHex.trim().toLowerCase();
  if (!/^[0-9a-f]{32}$/.test(challenge) || text(proof.purpose) !== input.purpose || text(proof.nonce).toLowerCase() !== challenge) return false;
  return verifyDaemonAssertion({
    purpose: input.purpose,
    daemonId: input.daemonId,
    daemonPublicKey: input.daemonPublicKey,
    timestamp: text(proof.timestamp),
    nonceHex: challenge,
    signatureHex: text(proof.signature),
  });
}

/**
 * The computers (daemon keys) this app has seen answer a challenge. Once one has, it is held to it: an answer without
 * a proof, a socket that proves nothing, is not that computer. A computer that has not yet is answered the old way
 * (an older mewla), only while that answer is fresh. Kept by key, so every pairing of that computer counts, and a
 * pairing removed and made again does not forget it.
 */
const PROVING_KEY = "mewla:v1:proving_daemons";
let proving: Promise<Set<string>> | null = null;

function provingKeys(): Promise<Set<string>> {
  proving ??= platform()
    .storage.getItem(PROVING_KEY)
    .then((raw) => {
      const parsed = JSON.parse(raw ?? "[]") as unknown;
      return new Set(Array.isArray(parsed) ? parsed.map((key) => normalizePublicKeyHex(String(key))).filter(Boolean) : []);
    })
    .catch(() => new Set<string>());
  return proving;
}

export async function daemonProves(daemonPublicKey: string): Promise<boolean> {
  const key = normalizePublicKeyHex(daemonPublicKey);
  return Boolean(key) && (await provingKeys()).has(key);
}

export async function noteDaemonProves(daemonPublicKey: string): Promise<void> {
  const key = normalizePublicKeyHex(daemonPublicKey);
  const keys = await provingKeys();
  if (!key || keys.has(key)) return;
  keys.add(key);
  await platform().storage.setItem(PROVING_KEY, JSON.stringify([...keys])).catch(() => {});
}

/** Tests: forget what was read from the store (a new app start). */
export function resetDaemonProofsForTests(): void {
  proving = null;
}

/**
 * How far from now an answer without a proof may be signed: the daemon refuses device signatures further off
 * (`clock-skew`, 5 min), so nothing that works today is lost, and an answer recorded earlier is useless.
 */
export const UNCHALLENGED_MAX_SKEW_MS = 5 * 60_000;

/**
 * Whether a daemon's answer is the computer's, now: its proof over this app's challenge (and the computer is held to
 * proofs from then on), or, from a computer that has never proved, its old assertion signed within minutes of now.
 * Older mewla answers the old way until it updates; the old way is accepted only for computers that never proved.
 */
export async function answerIsTheComputers(input: {
  purpose: ChallengePurpose;
  /** The old assertion's purpose ("mewla-pair", "mewla-probe"). */
  oldPurpose: string;
  challengeHex: string;
  daemonId: string;
  daemonPublicKey: string;
  body: { proof?: unknown; assertion_timestamp?: unknown; assertion_nonce?: unknown; assertion_signature?: unknown } | null | undefined;
}): Promise<boolean> {
  const body = input.body ?? {};
  if (body.proof !== undefined && body.proof !== null) {
    if (!(await proofAnswers({ ...input, proof: body.proof }))) return false;
    await noteDaemonProves(input.daemonPublicKey);
    return true;
  }
  if (await daemonProves(input.daemonPublicKey)) return false;
  const text = (value: unknown) => (typeof value === "string" ? value : "");
  const signedAt = Date.parse(text(body.assertion_timestamp));
  if (!Number.isFinite(signedAt) || Math.abs(Date.now() - signedAt) > UNCHALLENGED_MAX_SKEW_MS) return false;
  return verifyDaemonAssertion({
    purpose: input.oldPurpose,
    daemonId: input.daemonId,
    daemonPublicKey: input.daemonPublicKey,
    timestamp: text(body.assertion_timestamp),
    nonceHex: text(body.assertion_nonce),
    signatureHex: text(body.assertion_signature),
  });
}
