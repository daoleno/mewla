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

// Security review 11-23, finding 1: a link anyone can make named a computer this app had paired, at another host, and
// paired it there with no confirmation (a recorded `/pair` answer passes there). Now the confirmation comes, naming
// that host; a code at the address the app holds still re-pairs without one.
describe("a pairing link to a computer this app has, at another address", () => {
  const desk = testComputer("http://10.0.0.8:9876", 11);
  const deskElsewhere = testComputer("http://10.9.9.9:9876", 11);
  const withDesk = {
    ...paired,
    "mewla:v3:servers": JSON.stringify([
      ...JSON.parse(paired["mewla:v3:servers"]),
      { id: "s2", name: "Desk", url: "ws://10.0.0.8:9876/ws", daemonId: "a1".repeat(32), daemonPublicKey: desk.publicKey },
    ]),
  };
  const servers = (app: Awaited<ReturnType<typeof open>>) => JSON.parse(app.storage()["mewla:v3:servers"] ?? "[]") as { id: string; url: string }[];

  for (const [platform, word, ready] of [["android", "phone", "text:Brain is starting…"], ["macos", "Mac", "text:Waking up"]] as const) {
    test(`on ${platform}, asks first, naming the host`, async () => {
      const app = await open(APP, {
        host: phoneHost({ brain: {} }, { "app.pairing": () => ({ expires_at: "2099-01-01T00:00:00Z", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64) }) }),
        storage: withDesk, platform, ...(platform === "macos" ? { width: 1280, height: 800 } : {}),
        network: { [deskElsewhere.pairURL]: deskElsewhere.answer() },
      });
      const asked: string[] = [];
      app.onDialog((dialog) => {
        asked.push(`${dialog.title}\n${dialog.message}`);
        return "Cancel";
      });
      await app.waitFor(ready, { timeout: 10_000 });
      const link = deskElsewhere.link("Desk");
      if (platform === "macos") await app.openWithUrl(link);
      else await app.host.send("links.open", { url: link });
      await app.waitUntil(() => asked.length > 0, { timeout: 10_000 });
      expect(asked[0].startsWith(`Pair this ${word}?\n`)).toBe(true);
      expect(asked[0]).toContain(`access to 10.9.9.9:9876 (Desk),`);
      expect(servers(app).map((server) => server.url)).toEqual(["ws://127.0.0.1:1/ws", "ws://10.0.0.8:9876/ws"]);
      expect(app.storage()["mewla:v1:current_server_id"]).toBe("s1");
    }, 20_000);
  }

  test("at the address this app holds, re-pairs without asking", async () => {
    const app = await open(APP, { host: phoneHost({ brain: {} }), storage: withDesk, platform: "android", network: { [desk.pairURL]: desk.answer() } });
    const asked: string[] = [];
    app.onDialog((dialog) => {
      asked.push(dialog.title ?? "");
      return "Cancel";
    });
    await app.waitFor("text:Brain is starting…", { timeout: 10_000 });
    await app.host.send("links.open", { url: desk.link("Desk") });
    await app.waitFor("text:Connected to Desk.", { timeout: 10_000 });
    expect(app.storage()["mewla:v1:current_server_id"]).toBe("s2");
    expect(asked).toEqual([]);
    expect(servers(app).map((server) => server.id)).toEqual(["s2", "s1"]);
  }, 20_000);
});
