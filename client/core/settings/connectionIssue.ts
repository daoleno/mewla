import { buildAuthorizationHeader, normalizeDaemonId, normalizePublicKeyHex, verifyDaemonAssertion } from "../auth";
import { platform } from "../platform";
import type { StoredServer } from "../storedServerContract";

/**
 * Why the current computer's connection fails, in words a person can act on: after the
 * socket fails, the phone asks the computer's health and auth-check routes and names
 * the first thing that is wrong (the Expo app's diagnosis, step for step).
 */

export type ConnectionIssueCode =
  | "invalid_url"
  | "network_unreachable"
  | "device_not_paired"
  | "wrong_daemon"
  | "path_not_found"
  | "proxy_error"
  | "websocket_upgrade_failed"
  | "unexpected_http_response";

export interface ConnectionIssue {
  code: ConnectionIssueCode;
  title: string;
  detail: string;
  hint: string;
  httpStatus?: number;
}

interface TrustedDaemonPayload {
  daemon_id?: string;
  daemon_public_key?: string;
  assertion_timestamp?: string;
  assertion_nonce?: string;
  assertion_signature?: string;
}

const PROBE_TIMEOUT_MS = 4_000;

export interface DiagnoseDeps {
  fetch: (url: string, init: RequestInit) => Promise<Response>;
  authorization: (daemonId: string) => Promise<string>;
}

const defaultDeps: DiagnoseDeps = {
  fetch: (url, init) => platform().fetch(url, init),
  authorization: (daemonId) => buildAuthorizationHeader({ daemonId, purpose: "mewla-probe" }),
};

export async function diagnoseConnectionIssue(
  input: { server: StoredServer; socketError?: string },
  deps: DiagnoseDeps = defaultDeps,
): Promise<ConnectionIssue> {
  const daemonId = normalizeDaemonId(input.server.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.server.daemonPublicKey);
  const transportURL = input.server.url;
  const probeURL = buildProbeURL(transportURL);
  const healthURL = buildPathURL(transportURL, "/health");
  const authCheckURL = buildPathURL(transportURL, "/auth-check");

  if (!probeURL || !healthURL || !authCheckURL) {
    return issue("invalid_url", "Invalid server address", "The saved server address could not be read.", "Edit the server and enter its address, like https://mewla.example.com.");
  }
  if (!daemonId || !daemonPublicKey) {
    return issue(
      "wrong_daemon",
      "Missing daemon identity",
      "This saved server does not include a trusted daemon identity.",
      "Re-import the pairing link from mewla so Mewla can bind this endpoint to a daemon key.",
    );
  }
  const get = (url: string, headers?: Record<string, string>) => fetchWithTimeout(deps, url, { method: "GET", headers });

  try {
    const health = await get(healthURL);
    if (!health.ok) return mapProbeFailure(health.status, "health");
    if (!trusted(await readJSON(health), { purpose: "mewla-health", daemonId, daemonPublicKey })) {
      return issue(
        "wrong_daemon",
        "Wrong daemon identity",
        "The endpoint is reachable, but it did not prove possession of the trusted daemon key for this server.",
        "Re-import the pairing link for the correct daemon, or update the endpoint if your tunnel now points somewhere else.",
        health.status,
      );
    }

    const authCheck = await get(authCheckURL, { Authorization: await deps.authorization(daemonId) });
    if (authCheck.status === 401) {
      return issue(
        "device_not_paired",
        "Device is not paired",
        "The daemon is reachable and trusted, but it rejected this device identity.",
        "Import a fresh pairing link from mewla on this machine to enroll this phone again.",
        401,
      );
    }
    if (authCheck.status !== 200) return mapProbeFailure(authCheck.status, "auth-check");
    if (!trusted(await readJSON(authCheck), { purpose: "mewla-probe", daemonId, daemonPublicKey })) {
      return issue(
        "wrong_daemon",
        "Wrong daemon identity",
        "The endpoint answered the auth check, but its identity proof did not match the trusted daemon.",
        "Update the saved endpoint or re-import the pairing link from the daemon you actually want to trust.",
        authCheck.status,
      );
    }

    const probe = await get(probeURL, { Authorization: await deps.authorization(daemonId) });
    if (probe.status === 401) {
      return issue(
        "device_not_paired",
        "Device is not paired",
        "The server answered, but mewla there does not recognize this device.",
        "Import a fresh pairing link from mewla on this machine to enroll this phone again.",
        401,
      );
    }
    if (probe.status !== 200) return mapProbeFailure(probe.status, "ws");
    if (!trusted(await readJSON(probe), { purpose: "mewla-probe", daemonId, daemonPublicKey })) {
      return issue(
        "wrong_daemon",
        "Wrong daemon identity",
        "The server answered, but it did not prove the trusted daemon identity for this server.",
        "Make sure the tunnel forwards to the correct mewla instance rather than another service or machine.",
        probe.status,
      );
    }
    return issue(
      "websocket_upgrade_failed",
      "WebSocket session failed after probes passed",
      "HTTP reachability, daemon identity, and device authorization all succeeded, but the live WebSocket session still failed to open." +
        (input.socketError ? ` The phone's socket reported: ${input.socketError}.` : ""),
      "Check that your tunnel or reverse proxy forwards WebSocket Upgrade and Connection headers, and does not downgrade the connection to plain HTTP polling.",
      probe.status,
    );
  } catch (error) {
    return networkIssue(error);
  }
}

function mapProbeFailure(status: number, route: "health" | "auth-check" | "ws"): ConnectionIssue {
  switch (status) {
    case 404:
      return issue(
        "path_not_found",
        "Daemon routes are not exposed",
        `The endpoint returned HTTP 404 for mewla's ${route} route.`,
        "Forward the full mewla origin through your tunnel or reverse proxy, including /health, /auth-check, /pair, /upload, and /ws.",
        status,
      );
    case 400:
    case 405:
    case 426:
      return issue(
        "websocket_upgrade_failed",
        "WebSocket handshake failed",
        `The ${route} request reached the endpoint, but the tunnel or proxy did not behave like mewla expects.`,
        "Check HTTPS/TLS and make sure your reverse proxy preserves WebSocket Upgrade and Connection headers all the way to mewla.",
        status,
      );
    case 502:
    case 503:
    case 504:
      return issue(
        "proxy_error",
        "Proxy could not reach daemon",
        `The endpoint returned HTTP ${status}, which usually means your proxy or tunnel could not reach mewla on localhost.`,
        "Check that mewla is running on 127.0.0.1:9876 and that your tunnel points at the same local port.",
        status,
      );
    default:
      return issue(
        "unexpected_http_response",
        "Unexpected server response",
        `The endpoint returned HTTP ${status} instead of a valid mewla response.`,
        "Make sure this URL points at mewla rather than a website, API gateway, or some other service.",
        status,
      );
  }
}

function buildProbeURL(serverUrl: string): string | null {
  const trimmed = serverUrl.trim();
  if (!trimmed) return null;
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === "ws:") parsed.protocol = "http:";
    else if (parsed.protocol === "wss:") parsed.protocol = "https:";
    else if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}

function buildPathURL(serverUrl: string, pathname: string): string | null {
  const probe = buildProbeURL(serverUrl);
  if (!probe) return null;
  const parsed = new URL(probe);
  parsed.pathname = pathname;
  return parsed.toString();
}

async function fetchWithTimeout(deps: DiagnoseDeps, url: string, init: RequestInit): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
  try {
    return await deps.fetch(url, { ...init, signal: controller.signal });
  } catch (error) {
    throw controller.signal.aborted ? Object.assign(new Error("timed out"), { name: "AbortError" }) : error;
  } finally {
    clearTimeout(timer);
  }
}

async function readJSON(response: Response): Promise<TrustedDaemonPayload | null> {
  try {
    return (await response.json()) as TrustedDaemonPayload;
  } catch {
    return null;
  }
}

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function trusted(payload: TrustedDaemonPayload | null, input: { purpose: string; daemonId: string; daemonPublicKey: string }): boolean {
  if (!payload) return false;
  const daemonId = normalizeDaemonId(text(payload.daemon_id));
  const daemonPublicKey = normalizePublicKeyHex(text(payload.daemon_public_key));
  if (daemonId !== input.daemonId || daemonPublicKey !== input.daemonPublicKey) return false;
  return verifyDaemonAssertion({
    purpose: input.purpose,
    daemonId,
    daemonPublicKey,
    timestamp: text(payload.assertion_timestamp),
    nonceHex: text(payload.assertion_nonce),
    signatureHex: text(payload.assertion_signature),
  });
}

function networkIssue(error: unknown): ConnectionIssue {
  const raw = error instanceof Error ? error.message.trim() : "";
  const lower = raw.toLowerCase();
  if (error instanceof Error && error.name === "AbortError") {
    return issue("network_unreachable", "Server timed out", "The daemon did not respond before the probe timed out.", "Check tunnel latency, firewall rules, and whether mewla is sleeping or overloaded.");
  }
  if (lower.includes("enotfound") || lower.includes("name resolution") || lower.includes("dns") || lower.includes("host not found")) {
    return issue(
      "network_unreachable",
      "DNS lookup failed",
      raw || "The hostname could not be resolved before Mewla reached the daemon.",
      "Check the hostname spelling, your DNS configuration, or try a different tunnel hostname.",
    );
  }
  if (lower.includes("econnrefused") || lower.includes("connection refused")) {
    return issue(
      "network_unreachable",
      "Daemon refused connection",
      "The host is reachable, but nothing accepted the connection on the configured port.",
      "Check that mewla is running and that your tunnel or reverse proxy forwards to 127.0.0.1:9876.",
    );
  }
  if (lower.includes("certificate") || lower.includes("ssl") || lower.includes("tls")) {
    return issue(
      "network_unreachable",
      "TLS handshake failed",
      raw || "The HTTPS/TLS handshake failed before the WebSocket could connect.",
      "Check your certificate chain, hostname, and tunnel or reverse proxy TLS configuration.",
    );
  }
  return issue(
    "network_unreachable",
    "Server unreachable",
    raw
      ? `The network request failed before a WebSocket connection could be established: ${raw}`
      : "The network request failed before a WebSocket connection could be established.",
    "Check the hostname, DNS, tunnel, firewall, and whether mewla is running.",
  );
}

function issue(code: ConnectionIssueCode, title: string, detail: string, hint: string, httpStatus?: number): ConnectionIssue {
  return httpStatus === undefined ? { code, title, detail, hint } : { code, title, detail, hint, httpStatus };
}
