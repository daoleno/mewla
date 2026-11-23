import { deviceBinding, normalizePublicKeyHex, verifyDaemonAssertion } from "./deviceAuthContract";
import { verifyHealthChallenge } from "./link/directAddresses";
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

/** The line an app shows for a computer it holds to proofs that answers the old way: it runs a Mewla from before them. */
export function olderMewlaLine(name: string): string {
  const computer = name.trim() || "This computer";
  return `${computer} runs an older Mewla. Update Mewla on ${name.trim() || "it"}.`;
}

type AnswerBody = { proof?: unknown; assertion_timestamp?: unknown; assertion_nonce?: unknown; assertion_signature?: unknown } | null | undefined;

/**
 * What a daemon's answer is: "proved" (its proof over this app's challenge; the computer is held to proofs from then
 * on), "old" (no proof, from a computer that never proved: its old assertion, signed within minutes of now), "older"
 * (the same from a computer held to proofs: its own key, now, so it runs a Mewla from before proofs, a downgrade), or
 * "refused" (anything else: a recorded answer, another key, a bad proof).
 */
export async function daemonAnswer(input: {
  purpose: ChallengePurpose;
  /** The old assertion's purpose ("mewla-pair", "mewla-probe"). */
  oldPurpose: string;
  challengeHex: string;
  daemonId: string;
  daemonPublicKey: string;
  body: AnswerBody;
}): Promise<"proved" | "old" | "older" | "refused"> {
  const body = input.body ?? {};
  if (body.proof !== undefined && body.proof !== null) {
    if (!(await proofAnswers({ ...input, proof: body.proof }))) return "refused";
    await noteDaemonProves(input.daemonPublicKey);
    return "proved";
  }
  const text = (value: unknown) => (typeof value === "string" ? value : "");
  const signedAt = Date.parse(text(body.assertion_timestamp));
  if (!Number.isFinite(signedAt) || Math.abs(Date.now() - signedAt) > UNCHALLENGED_MAX_SKEW_MS) return "refused";
  const genuine = await verifyDaemonAssertion({
    purpose: input.oldPurpose,
    daemonId: input.daemonId,
    daemonPublicKey: input.daemonPublicKey,
    timestamp: text(body.assertion_timestamp),
    nonceHex: text(body.assertion_nonce),
    signatureHex: text(body.assertion_signature),
  });
  if (!genuine) return "refused";
  return (await daemonProves(input.daemonPublicKey)) ? "older" : "old";
}

/**
 * Whether a daemon's answer is the computer's, now ("proved" or "old", `daemonAnswer`). Older mewla answers the old
 * way until it updates; the old way is accepted only for computers that never proved.
 */
export async function answerIsTheComputers(input: Parameters<typeof daemonAnswer>[0]): Promise<boolean> {
  const answer = await daemonAnswer(input);
  return answer === "proved" || answer === "old";
}

/**
 * Whether the computer at `url` (the address a connect just failed at) is this computer running a Mewla from before
 * proofs: its key answers this app's /health challenge (older Mewla does that too), and it does not say it proves
 * itself (`connect_proof`). Unreachable, or another computer: false.
 */
export async function runsOlderMewla(server: { daemonId: string; daemonPublicKey: string }, url: string): Promise<boolean> {
  let health: string;
  try {
    const address = new URL(url);
    address.protocol = address.protocol === "wss:" ? "https:" : address.protocol === "ws:" ? "http:" : address.protocol;
    address.pathname = "/health";
    address.search = "";
    address.hash = "";
    health = address.toString();
  } catch {
    return false;
  }
  const challengeHex = newChallenge();
  try {
    const response = await platform().fetch(`${health}?challenge=${challengeHex}`, { method: "GET" });
    if (!response.ok) return false;
    const payload = (await response.json()) as Record<string, unknown>;
    if (payload.connect_proof === true) return false;
    return await verifyHealthChallenge({ challengeHex, daemonId: server.daemonId, daemonPublicKey: server.daemonPublicKey, payload });
  } catch {
    return false;
  }
}

/**
 * The binding this app's device headers for `server` name at `url` (the address the request goes to), or null for an
 * unbound (v1) header: only a computer that has proved signs challenges, and so takes bound headers (older mewla
 * refuses them). A Relay pairing is always reached through a tunnel pinned to its transport key, so it binds that key.
 */
export async function bindingFor(server: { daemonPublicKey: string; transportKind?: string; transportPin?: string }, url: string): Promise<string | null> {
  if (!(await daemonProves(server.daemonPublicKey))) return null;
  return server.transportKind === "link" ? deviceBinding({ transportPin: server.transportPin }) : deviceBinding({ url });
}
