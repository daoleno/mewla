// App Store Connect webhook receiver (worker.mjs serves it as a Cloudflare Worker): a Mewla Preview build changed state,
// so the TestFlight pipeline runs now instead of on a schedule.
//
// App Store Connect POSTs to /app-store-connect, signed with the webhook's secret in
// `x-apple-signature: hmacsha256=<hex HMAC-SHA256 of the raw body>`. A verified event that moves the
// pipeline dispatches ios-testflight-review.yml on main (workflow_dispatch, so the token needs only
// Actions: write on the one repository): the review queue submits the next waiting build and the
// signed availability manifest is published once a build is in beta testing.
//
//   buildUploadStateUpdated, newState COMPLETE     an upload finished processing: submit it if free
//   buildBetaDetailExternalBuildStateUpdated       Beta App Review approved/rejected a build, or it
//                                                  entered testing: submit the next one, publish
//
// Anything else that verifies (a ping from POST /v1/webhookPings, other event types) is answered 200
// and dispatches nothing. A failed dispatch answers 502, so App Store Connect's delivery log shows it.
//
// Secrets: ASC_WEBHOOK_SECRET (the webhook's secret), GITHUB_TOKEN (fine-grained, daoleno/mewla,
// Actions: read and write). Vars (wrangler.toml): GITHUB_REPOSITORY, GITHUB_WORKFLOW, GITHUB_REF.
// Runbook: docs/ios-ci-release.md, "App Store Connect webhook".

export const PATH = "/app-store-connect";
export const MAX_BODY_BYTES = 64 * 1024;
const SIGNATURE_PREFIX = "hmacsha256=";

/** Whether this verified event should run the TestFlight pipeline. */
export function shouldDispatch(event) {
  const data = event && typeof event === "object" ? event.data : null;
  if (!data || typeof data !== "object") return false;
  if (data.type === "buildBetaDetailExternalBuildStateUpdated") return true;
  if (data.type === "buildUploadStateUpdated") return data.attributes?.newState === "COMPLETE";
  return false;
}

function hexBytes(hex) {
  if (typeof hex !== "string" || hex.length !== 64 || !/^[0-9a-fA-F]+$/.test(hex)) return null;
  const bytes = new Uint8Array(32);
  for (let i = 0; i < 32; i++) bytes[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return bytes;
}

/** Verify `hmacsha256=<hex>` over the exact body bytes (crypto.subtle.verify compares in constant time). */
export async function verifySignature(secret, body, header) {
  if (!secret || typeof header !== "string" || !header.startsWith(SIGNATURE_PREFIX)) return false;
  const signature = hexBytes(header.slice(SIGNATURE_PREFIX.length).trim());
  if (!signature) return false;
  const key = await crypto.subtle.importKey(
    "raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["verify"],
  );
  return crypto.subtle.verify("HMAC", key, signature, body);
}

/** Run ios-testflight-review.yml on main; the event type goes in as its asc_event input. */
export async function dispatch(env, eventType, send) {
  const repository = env.GITHUB_REPOSITORY || "daoleno/mewla";
  const workflow = env.GITHUB_WORKFLOW || "ios-testflight-review.yml";
  const response = await send(
    `https://api.github.com/repos/${repository}/actions/workflows/${workflow}/dispatches`,
    {
      method: "POST",
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${env.GITHUB_TOKEN}`,
        "Content-Type": "application/json",
        "User-Agent": "mewla-asc-webhook",
        "X-GitHub-Api-Version": "2022-11-28",
      },
      body: JSON.stringify({ ref: env.GITHUB_REF || "main", inputs: { asc_event: eventType } }),
    },
  );
  return response.status;
}

const text = (status, body) => new Response(body + "\n", { status, headers: { "Content-Type": "text/plain" } });

export async function handle(request, env, send = fetch) {
  const url = new URL(request.url);
  if (url.pathname !== PATH) return text(404, "not found");
  if (request.method !== "POST") return text(405, "POST only");
  const length = Number(request.headers.get("content-length") || "0");
  if (length > MAX_BODY_BYTES) return text(413, "too large");
  const body = new Uint8Array(await request.arrayBuffer());
  if (body.byteLength > MAX_BODY_BYTES) return text(413, "too large");
  if (!(await verifySignature(env.ASC_WEBHOOK_SECRET, body, request.headers.get("x-apple-signature")))) {
    return text(401, "bad signature");
  }
  let event;
  try {
    event = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(body));
  } catch {
    return text(400, "not JSON");
  }
  const type = typeof event?.data?.type === "string" ? event.data.type : "unknown";
  if (!shouldDispatch(event)) return text(200, `ignored ${type}`);
  if (!env.GITHUB_TOKEN) return text(503, `dispatch not configured for ${type}`);
  let status;
  try {
    status = await dispatch(env, type, send);
  } catch (error) {
    return text(502, `dispatch failed: ${error?.message ?? error}`);
  }
  if (status < 200 || status >= 300) return text(502, `dispatch refused: GitHub HTTP ${status}`);
  return text(202, `dispatched ${type}`);
}
