import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const request = { request_id: "r1", verification_number: "042", device_name: "Firefox on Linux", platform: "web", origin: "https://desk.example.ts.net", expires_at: "2099-01-01T00:00:00Z", status: "pending" };

describe("a new device asks to be let in", () => {
  test("a paired phone picks the number the device shows and approves it", async () => {
    const app = await open(APP, {
      host: phoneHost({ brain: {} }),
      storage: paired,
      platform: "android",
      network: {
        ...expoTokenService,
        "GET http://127.0.0.1:1/enrollment/pending": { status: 200, body: { requests: [request] } },
        "POST http://127.0.0.1:1/enrollment/decision": { status: 200, body: {} },
      },
    });
    await app.waitFor("id:enrollment-approval");
    expect(app.texts()).toContain("Firefox on Linux");
    expect(app.texts()).toContain("web · https://desk.example.ts.net");
    expect(app.count("text:042")).toBe(1);
    await app.press("id:enrollment-choice-042");
    await app.press("id:enrollment-approve");
    await app.waitFor("id:enrollment-approval", { state: "gone" });
  }, 20_000);

  test("closed undecided, the request waits in the tray: one more needs you, and its row opens the sheet again", async () => {
    const app = await open(APP, {
      host: phoneHost({ brain: {} }),
      storage: paired,
      platform: "android",
      network: {
        ...expoTokenService,
        "GET http://127.0.0.1:1/enrollment/pending": { status: 200, body: { requests: [request] } },
        "POST http://127.0.0.1:1/enrollment/decision": { status: 200, body: {} },
      },
    });
    await app.waitFor("id:enrollment-approval");
    await app.back();
    await app.waitFor("id:enrollment-approval", { state: "gone" });
    await app.waitFor("text:1 needs you");
    await app.press("id:state-control");
    await app.waitFor("id:tray-needs");
    expect(app.texts()).toContain("Approve new device");
    expect(app.texts()).toContain("Firefox on Linux");
    await app.press("id:tray-needs >> id:tray-request");
    await app.waitFor("id:enrollment-approval");
    await app.press("id:enrollment-choice-042");
    await app.press("id:enrollment-approve");
    await app.waitFor("id:enrollment-approval", { state: "gone" });
    await app.waitUntil(() => !app.texts().includes("1 needs you"));
  }, 20_000);

  test("a browser a pairing link paired withdraws its own request instead of asking to approve it", async () => {
    const own = { requestId: "r1", secret: "s", verificationNumber: "042", expiresAt: "2099-01-01T00:00:00Z" };
    const app = await open(APP, {
      host: phoneHost({ brain: {} }),
      storage: { ...paired, "mewla:enrollment:v1": JSON.stringify(own) },
      platform: "android",
      network: {
        ...expoTokenService,
        "GET http://127.0.0.1:1/enrollment/pending": { status: 200, body: { requests: [request] } },
        "POST http://127.0.0.1:1/enrollment/decision": { status: 200, body: {} },
      },
    });
    const decisions = () => app.requests().filter((r) => r.url.endsWith("/enrollment/decision"));
    await app.waitUntil(() => decisions().length === 1, { timeout: 10_000 });
    expect(JSON.parse(decisions()[0].body ?? "{}")).toMatchObject({ request_id: "r1", approve: false });
    expect(app.shows("id:enrollment-approval")).toBe(false);
    expect(app.storage()["mewla:enrollment:v1"]).toBeUndefined();
  }, 20_000);

  test("a browser that isn't paired shows its number and joins once approved", async () => {
    let polls = 0;
    const app = await open(APP, {
      host: { params: { daemon: { brain: {} } }, handlers: { "app.origin": () => "https://desk.example.ts.net", "links.initial": () => null, "app.legacyStorage": () => null, "app.legacyStorageCopied": () => null, "secure.get": () => null, "secure.set": () => null, "app.testFrame": () => null } },
      platform: "web",
      network: {
        "POST https://desk.example.ts.net/enrollment/request": { status: 200, body: { request_id: "r1", secret: "s", verification_number: "317", expires_at: "2099-01-01T00:00:00Z" } },
        "GET https://desk.example.ts.net/enrollment/status?id=r1&secret=s": () => {
          polls += 1;
          return { status: 200, body: polls < 2 ? { status: "pending" } : { status: "approved", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64), device_id: "d" } };
        },
      },
    });
    await app.waitFor("id:join-number");
    expect(app.texts()).toContain("317");
    expect(app.texts().join(" ")).toContain("mewla devices approve 317");
    await app.waitFor("id:browser-join", { state: "gone", timeout: 10_000 });
  }, 20_000);
});
