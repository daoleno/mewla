// node --test scripts/asc-webhook/receiver.test.mjs (run by scripts/tests/test_asc_webhook.py).
import assert from "node:assert/strict";
import { createHmac } from "node:crypto";
import { test } from "node:test";
import { MAX_BODY_BYTES, handle, shouldDispatch, verifySignature } from "./receiver.mjs";

const SECRET = "test-secret";
const ENV = { ASC_WEBHOOK_SECRET: SECRET, GITHUB_TOKEN: "github_pat_test", GITHUB_REPOSITORY: "daoleno/mewla",
  GITHUB_WORKFLOW: "ios-testflight-review.yml", GITHUB_REF: "main" };
const sign = (body, secret = SECRET) => "hmacsha256=" + createHmac("sha256", secret).update(body).digest("hex");

// The payloads Apple documents (developer.apple.com/documentation/appstoreconnectapi/webhook-events).
const APPROVED = JSON.stringify({ data: { type: "buildBetaDetailExternalBuildStateUpdated", id: "4a9eacca", version: 1,
  attributes: { newExternalBuildState: "BETA_APPROVED", oldExternalBuildState: "IN_BETA_REVIEW", timestamp: "2025-04-16T05:00:52.745Z" },
  relationships: { instance: { data: { type: "buildBetaDetails", id: "ad7e6298" } } } } });
const UPLOADED = JSON.stringify({ data: { type: "buildUploadStateUpdated", id: "7c813492", version: 1,
  attributes: { newState: "COMPLETE" }, relationships: { instance: { data: { type: "buildUploads", id: "ad7e6298" } } } } });

function post(body, headers = {}, path = "/app-store-connect") {
  return new Request(`https://asc.example${path}`, { method: "POST", body, headers: { "x-apple-signature": sign(body), ...headers } });
}

function github(status = 204) {
  const calls = [];
  const send = async (url, init) => { calls.push({ url, init }); return new Response(null, { status }); };
  return { calls, send };
}

test("Apple's documented test vector verifies", async () => {
  const body = new TextEncoder().encode("Hello, World!");
  const header = "hmacsha256=7f062172b01cb00b53ca068614674a3d982a34062a0f5d37687d5e3377e54657";
  assert.equal(await verifySignature("This is my secret", body, header), true);
  assert.equal(await verifySignature("This is my secret", body, header.toUpperCase().replace("HMACSHA256", "hmacsha256")), true);
  assert.equal(await verifySignature("another secret", body, header), false);
  assert.equal(await verifySignature("This is my secret", body, header.replace("hmacsha256=", "sha256=")), false);
  assert.equal(await verifySignature("This is my secret", body, "hmacsha256=7f06"), false);
  assert.equal(await verifySignature("", body, header), false);
});

test("a signed beta-state event dispatches the review workflow on main", async () => {
  const gh = github();
  const response = await handle(post(APPROVED), ENV, gh.send);
  assert.equal(response.status, 202);
  assert.equal(gh.calls.length, 1);
  const { url, init } = gh.calls[0];
  assert.equal(url, "https://api.github.com/repos/daoleno/mewla/actions/workflows/ios-testflight-review.yml/dispatches");
  assert.equal(init.method, "POST");
  assert.equal(init.headers.Authorization, "Bearer github_pat_test");
  assert.equal(init.headers["X-GitHub-Api-Version"], "2022-11-28");
  assert.deepEqual(JSON.parse(init.body), { ref: "main", inputs: { asc_event: "buildBetaDetailExternalBuildStateUpdated" } });
});

test("a finished upload dispatches; other upload states, pings and other events do not", async () => {
  const gh = github();
  assert.equal((await handle(post(UPLOADED), ENV, gh.send)).status, 202);
  const processing = JSON.stringify({ data: { type: "buildUploadStateUpdated", attributes: { newState: "PROCESSING" } } });
  const ping = JSON.stringify({ data: { type: "webhookPingCreated", id: "p1", attributes: { timestamp: "2025-04-16T05:00:52.745Z" } } });
  const feedback = JSON.stringify({ data: { type: "betaFeedbackCrashSubmissionCreated" } });
  for (const body of [processing, ping, feedback]) {
    const response = await handle(post(body), ENV, gh.send);
    assert.equal(response.status, 200);
    assert.match(await response.text(), /^ignored /);
  }
  assert.equal(gh.calls.length, 1);
  assert.equal(shouldDispatch(null), false);
  assert.equal(shouldDispatch({ data: "x" }), false);
});

test("an unsigned, wrongly signed or tampered request is refused before anything is parsed or sent", async () => {
  const gh = github();
  const unsigned = new Request("https://asc.example/app-store-connect", { method: "POST", body: APPROVED });
  assert.equal((await handle(unsigned, ENV, gh.send)).status, 401);
  assert.equal((await handle(post(APPROVED, { "x-apple-signature": sign(APPROVED, "wrong") }), ENV, gh.send)).status, 401);
  assert.equal((await handle(post(UPLOADED, { "x-apple-signature": sign(APPROVED) }), ENV, gh.send)).status, 401);
  assert.equal((await handle(post("not json"), ENV, gh.send)).status, 400);
  assert.equal(gh.calls.length, 0);
});

test("only POST to the webhook path, within the size limit", async () => {
  const gh = github();
  assert.equal((await handle(new Request("https://asc.example/app-store-connect"), ENV, gh.send)).status, 405);
  assert.equal((await handle(post(APPROVED, {}, "/"), ENV, gh.send)).status, 404);
  const big = JSON.stringify({ data: { type: "buildBetaDetailExternalBuildStateUpdated", pad: "x".repeat(MAX_BODY_BYTES) } });
  assert.equal((await handle(post(big), ENV, gh.send)).status, 413);
  assert.equal(gh.calls.length, 0);
});

test("a dispatch GitHub refuses, or one with no token configured, is not reported as delivered", async () => {
  assert.equal((await handle(post(APPROVED), ENV, github(401).send)).status, 502);
  assert.equal((await handle(post(APPROVED), ENV, async () => { throw new Error("offline"); })).status, 502);
  const { GITHUB_TOKEN, ...untokened } = ENV;
  assert.equal((await handle(post(APPROVED), untokened, github().send)).status, 503);
  // A ping still succeeds without the token, so the endpoint can be proven before it is set.
  const ping = JSON.stringify({ data: { type: "webhookPingCreated" } });
  assert.equal((await handle(post(ping), untokened, github().send)).status, 200);
});

test("the Worker entry exports only its fetch handler", async () => {
  const entry = await import("./worker.mjs");
  assert.deepEqual(Object.keys(entry), ["default"]);
  assert.equal(typeof entry.default.fetch, "function");
});
