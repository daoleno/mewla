import { describe, expect, open, test } from "nsh/test";
import { answering } from "../testing/fixtures";
import nacl from "../../core/vendor/nacl";
import { buildLinkPairingTestLink } from "../../core/testing/linkPairingTestLink";
import { readPairingLink } from "../../core/pairFlow";
import { testComputer } from "../testing/pairing";

const computer = testComputer();

function phone(camera: { permission?: string; request?: string } = {}, extra: Record<string, (data: any) => unknown> = {}) {
  const secure = new Map<string, string>();
  return {
    params: { daemon: {} },
    handlers: {
      "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
      "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
      "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
      "app.legacyStorage": () => null,
      "app.legacyStorageCopied": () => undefined,
      "app.crashReports": () => [],
      "app.openSettings": () => null,
      "app.share": () => null,
      "scan.permission": () => camera.permission ?? "denied",
      "scan.request": () => camera.request ?? "denied",
      ...extra,
    },
  };
}

describe("first run", () => {
  test("welcome, then the install command with Copy, then the scan step", async () => {
    const app = await open("../App.tsx", { host: phone(), platform: "android" });
    await app.waitFor("id:set-up");
    expect(app.texts().join(" ")).toContain("Naps all day.");
    expect(app.shows("id:scan-now")).toBe(true);
    await app.press("id:set-up");
    await app.waitFor("id:install-command");
    expect(app.texts()).toContain("curl -fsSL https://mewla.ai/install.sh | sh");
    await app.press("id:copy-command");
    expect(app.clipboard()).toBe("curl -fsSL https://mewla.ai/install.sh | sh");
    await app.waitFor("text:Copied");
    await app.press("id:share-command");
    expect(app.host.calls().some((call) => call.name === "app.share")).toBe(true);
    await app.press("id:next-scan");
    await app.waitFor("id:finder");
    expect(app.texts()).toContain("Scan the code in your terminal");
  });

  test("the scan step asks for the camera by itself; refused, it points to Settings and Paste still pairs", async () => {
    const app = await open("../App.tsx", { host: phone({ permission: "undetermined", request: "denied" }), platform: "android" });
    await app.press("id:scan-now");
    await app.waitFor("id:open-settings");
    expect(app.host.calls().map((call) => call.name)).toContain("scan.request");
    expect(app.texts().join(" ")).toContain("The camera is off for Mewla.");
    await app.press("id:open-settings");
    expect(app.host.calls().map((call) => call.name)).toContain("app.openSettings");
    expect(app.shows("id:paste")).toBe(true);
  });

  test("Other ways to connect opens the docs", async () => {
    const app = await open("../App.tsx", { host: phone(), platform: "android" });
    await app.press("id:scan-now");
    await app.press("id:other-ways");
    expect(app.openedUrls()).toContain("https://mewla.ai/docs/connect-and-pair/#choose-an-address");
  });
});

describe("pairing by paste", () => {
  test("an empty clipboard and a text that is not a code each say what to do", async () => {
    const app = await open("../App.tsx", { host: phone(), platform: "android" });
    await app.press("id:scan-now");
    await app.press("id:paste");
    await app.waitFor("text:Nothing to paste");
    const again = await open("../App.tsx", { host: phone(), platform: "android", clipboard: "hello" });
    await again.press("id:scan-now");
    await again.press("id:paste");
    await again.waitFor("text:Not a pairing code");
  });

  test("asks before granting access; Cancel changes nothing", async () => {
    const app = await open("../App.tsx", { host: phone(), platform: "android", clipboard: computer.link() });
    app.onDialog(() => "Cancel");
    await app.press("id:scan-now");
    await answering(app, () => app.press("id:paste"));
    expect(app.dialogs()[0]).toMatchObject({ title: "Pair this phone?" });
    expect(app.shows("id:pair-issue")).toBe(false);
    expect(app.storage()["mewla:v3:servers"]).toBeUndefined();
  });

  test("a used or expired code says so and keeps the scan step", async () => {
    const app = await open("../App.tsx", {
      host: phone(), platform: "android", clipboard: computer.link(),
      network: { [computer.pairURL]: { status: 401, body: "pairing code expired" } },
    });
    app.onDialog(() => "Pair and grant access");
    await app.press("id:scan-now");
    await answering(app, () => app.press("id:paste"));
    await app.waitFor("text:Code used or expired");
    expect(app.texts()).toContain("pairing code expired");
    expect(app.shows("id:finder")).toBe(true);
  });

  test("an unreachable computer says where and what to check", async () => {
    const app = await open("../App.tsx", {
      host: phone(), platform: "android", clipboard: computer.link(),
      network: { [computer.pairURL]: { error: "Network request failed" } },
    });
    app.onDialog(() => "Pair and grant access");
    await app.press("id:scan-now");
    await answering(app, () => app.press("id:paste"));
    await app.waitFor("text:Can't reach your computer");
    expect(app.texts().join(" ")).toContain("10.0.0.5:9876");
  });
});

describe("after pairing", () => {
  test("the connected step: Brain is home, then Open Brain", async () => {
    const app = await open("../FakeDaemonApp.tsx", {
      host: phone(), platform: "android", clipboard: computer.link(),
      network: { [computer.pairURL]: () => computer.answer() },
    });
    app.onDialog(() => "Pair and grant access");
    await app.press("id:scan-now");
    await answering(app, () => app.press("id:paste"));
    await app.waitFor("text:Brain is home.", { timeout: 10_000 });
    expect(app.texts()).toContain("Connected to Desk.");
    expect(app.storage()["mewla:onboarded"]).toBe("true");
    await app.press("id:open-brain");
    // Brain's composer is there behind the onboarding already: wait for the onboarding to go.
    await app.waitFor("id:onboarding", { state: "gone" });
    await app.waitFor("id:composer");
  }, 15_000);

  test("a computer that can't be reached offers Retry and Server settings", async () => {
    const app = await open("../App.tsx", {
      host: phone(), platform: "android", clipboard: computer.link(),
      network: { [computer.pairURL]: () => computer.answer() },
    });
    app.onDialog(() => "Pair and grant access");
    await app.press("id:scan-now");
    await answering(app, () => app.press("id:paste"));
    await app.waitFor("id:connected-title");
    await app.waitFor("text:Can't reach your computer", { timeout: 15_000 });
    expect(app.shows("id:retry")).toBe(true);
    await app.press("id:server-settings");
    await app.waitFor("id:pair-another");
  }, 20_000);

  test("an opened pairing link pairs the same way and shows the connected step", async () => {
    const app = await open("../FakeDaemonApp.tsx", { host: phone(), platform: "android", network: { [computer.pairURL]: () => computer.answer() } });
    app.onDialog(() => "Pair and grant access");
    await app.waitFor("id:set-up");
    await answering(app, () => app.host.send("links.open", { url: computer.link() }));
    await app.waitFor("text:Brain is home.", { timeout: 10_000 });
  }, 15_000);
});

describe("Mewla Relay", () => {
  test("a Link code pairs through the host's pinned tunnel, says where it is going, and keeps the route", async () => {
    // A computer of its own (seed 9): nothing another test paired may stand in for it.
    const relayComputer = testComputer("http://127.0.0.1:4100", 9);
    const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(9));
    const link = buildLinkPairingTestLink({
      secretKey: keys.secretKey, publicKey: keys.publicKey, enrollmentToken: "2".repeat(64),
      routeId: "3".repeat(32), transportPin: "4".repeat(64), label: "quiet-otter-3f9a", domain: "a.link.test", name: "region-a",
      admissionAlias: "5".repeat(32), expiresAtMs: Date.now() + 3_600_000,
    });
    const app = await open("../App.tsx", {
      host: phone({}, { "link.start": () => ({ port: 4100, rttMs: 20 }), "link.stop": () => null, "link.events": () => [] }),
      platform: "android", clipboard: link,
      network: { [relayComputer.pairURL]: () => relayComputer.answer() },
    });
    app.onDialog(() => "Pair and grant access");
    await app.press("id:scan-now");
    await app.waitFor("id:paste");
    await answering(app, () => app.press("id:paste"));
    // Each stage on its own, so a slow runner says where it stopped.
    await app.waitUntil(() => app.dialogs().length > 0 || app.host.calls().some((call) => call.name === "link.start"), { timeout: 10_000 }).catch((error) => {
      const parsed = readPairingLink(link);
      throw new Error(`${error}; now=${Date.now()} expires=${parsed ? "ok" : "unparsed"} clipboard=${(app.clipboard() ?? "").length} calls=${app.host.calls().map((call) => call.name).join(",")}`);
    });
    await app.waitUntil(() => app.host.calls().some((call) => call.name === "link.start") || app.shows("id:pair-issue"), { timeout: 10_000 });
    expect(app.shows("id:pair-issue") ? app.texts().join(" | ") : "").toBe("");
    await app.waitFor("id:connected-title", { timeout: 15_000 });
    const calls = app.host.calls();
    expect(calls.find((call) => call.name === "link.start")?.data).toEqual({
      key: `pair:${"3".repeat(32)}`, host: `${"5".repeat(32)}.a.link.test`, port: 443, pin: "4".repeat(64), mode: "on-demand",
    });
    expect(calls.some((call) => call.name === "link.stop")).toBe(true);
    const saved = JSON.parse(app.storage()["mewla:v3:servers"]!)[0];
    expect(saved).toMatchObject({ url: "wss://quiet-otter-3f9a.a.link.test/ws", transportKind: "link", transportPin: "4".repeat(64), linkRouteId: "3".repeat(32) });
  }, 45_000);
});

describe("Settings' Pair a computer", () => {
  const desk = { id: "s1", name: "Desk", url: "ws://10.0.0.9:9876/ws", daemonId: "b2".repeat(32), daemonPublicKey: "c".repeat(64) };
  const paired = { "mewla:v3:servers": JSON.stringify([desk]), "mewla:v1:current_server_id": "s1", "mewla:onboarded": "true" };

  async function pairPage(extra: Record<string, (data: any) => unknown> = {}, options: Record<string, unknown> = {}) {
    const app = await open("../App.tsx", { host: phone({}, extra), platform: "android", storage: paired, ...options });
    await app.host.send("links.open", { url: "mewla://settings?addServer=1" });
    await app.waitFor("id:pair-link");
    return app;
  }

  test("a link typed into the field pairs, and the connected step follows", async () => {
    const app = await pairPage({}, { network: { [computer.pairURL]: () => computer.answer() } });
    app.onDialog(() => "Pair and grant access");
    await app.type("id:pair-link", computer.link());
    await app.press("id:pair-link-submit");
    await app.waitFor("id:connected-title", { timeout: 10_000 });
    expect(JSON.parse(app.storage()["mewla:v3:servers"]!).map((s: { url: string }) => s.url).sort()).toEqual(["ws://10.0.0.5:9876/ws", desk.url]);
  }, 15_000);

  test("Pair with an empty field reads the clipboard; with nothing there it asks for the link", async () => {
    const app = await pairPage();
    await app.press("id:pair-link-submit");
    await app.waitFor("text:Pairing link required");
  });

  test("Pick QR image pairs the code in the picture, and says when a picture holds none", async () => {
    let answer: unknown = { value: null };
    const app = await pairPage({ "scan.image": () => answer });
    app.onDialog(() => "Cancel");
    await app.press("id:pick-qr-image");
    await app.waitFor("text:No QR code found");
    answer = { value: computer.link() };
    await app.press("id:pick-qr-image");
    await app.waitUntil(() => app.dialogs().length > 0);
    expect(app.dialogs()[0]).toMatchObject({ title: "Pair this phone?" });
  });
});

