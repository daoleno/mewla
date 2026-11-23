import { describe, expect, open, test } from "nsh/test";
import { answering, paired, phoneHost } from "../testing/fixtures";
import { testComputer } from "../testing/pairing";
import nacl from "../../core/vendor/nacl";
import { buildLinkPairingTestLink } from "../../core/testing/linkPairingTestLink";

const APP = "../FakeDaemonApp.tsx";
const computer = testComputer("http://10.0.0.8:9876", 11);

describe("a pairing link opened while the app runs (parity #36)", () => {
  // (While it pairs the note says "Pairing…"; `nsh test` steps wait for the request, so that is for the device.)
  test("says why a used code did not pair, over the app, and the note goes when tapped", async () => {
    const app = await open(APP, {
      host: phoneHost({ brain: {} }), storage: paired, platform: "android",
      network: { [computer.pairURL]: { status: 401, body: "pairing code already used" } },
    });
    app.onDialog(() => "Pair and grant access");
    await app.waitFor("text:Brain is starting…", { timeout: 10_000 });
    await answering(app, () => app.host.send("links.open", { url: computer.link("Lab") }));
    await app.waitFor("id:link-pairing", { timeout: 10_000 });
    await app.waitFor("text:Code used or expired", { timeout: 10_000 });
    await app.press("id:link-pairing");
    await app.waitUntil(() => !app.shows("id:link-pairing"));
    expect(app.storage()["mewla:v1:current_server_id"]).toBe("s1");
  }, 20_000);
});

// Owner issue 11-23 ("mewla: 协议的 computer 无法连接"): Mewla for Mac claimed no scheme and listened only to the phone
// hosts' `links.open`, so a `mewla://` link the system opened never reached it, at launch or while it ran.
describe("a pairing link the system opens on a Mac", () => {
  const thisMac = { expires_at: "2099-01-01T00:00:00Z", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64) };
  const macHost = () => phoneHost({ brain: {} }, { "app.pairing": () => thisMac });
  const pairedLab = (app: Awaited<ReturnType<typeof open>>) => (JSON.parse(app.storage()["mewla:v3:servers"] ?? "[]") as { name: string; url: string }[])
    .some((server) => server.name === "Lab" && server.url.includes("10.0.0.8:9876"));

  test("while it runs, the link pairs that computer", async () => {
    const app = await open(APP, { host: macHost(), storage: paired, platform: "macos", width: 1280, height: 800, network: { [computer.pairURL]: computer.answer() } });
    app.onDialog(() => "Pair and grant access");
    await app.waitFor("text:Waking up", { timeout: 10_000 });
    await app.openWithUrl(computer.link("Lab"));
    await app.waitUntil(() => pairedLab(app), { timeout: 10_000 });
  }, 20_000);

  // The confirmation comes before a test can answer it (`onDialog` answers dialogs from then on): it was asked, so the
  // link reached pairing; it is cancelled, so nothing was paired.
  test("the link that launched it asks to pair that computer once the app has started", async () => {
    const app = await open(APP, { host: macHost(), storage: paired, platform: "macos", width: 1280, height: 800, urlScheme: computer.link("Lab") });
    await app.waitUntil(() => app.dialogs().some((dialog) => dialog.title?.startsWith("Pair this")), { timeout: 10_000 });
    expect(pairedLab(app)).toBe(false);
  }, 20_000);
});

describe("an expired Mewla Relay code opened while the app runs", () => {
  test("says it expired, over the app, instead of opening Settings", async () => {
    const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(9));
    const expired = buildLinkPairingTestLink({
      secretKey: keys.secretKey, publicKey: keys.publicKey, enrollmentToken: "2".repeat(64), routeId: "3".repeat(32), transportPin: "4".repeat(64),
      label: "quiet-otter-3f9a", domain: "m.mewla.ai", admissionAlias: "5".repeat(32), expiresAtMs: Date.now() - 60_000,
    });
    const app = await open(APP, { host: phoneHost({ brain: {} }), storage: paired, platform: "android" });
    await app.waitFor("text:Brain is starting…", { timeout: 10_000 });
    await app.host.send("links.open", { url: expired });
    await app.waitFor("id:link-pairing", { timeout: 10_000 });
    await app.waitFor("text:Code expired", { timeout: 10_000 });
  }, 20_000);
});
