import { describe, expect, open, test } from "nsh/test";
import { answering, paired, phoneHost } from "../testing/fixtures";
import { testComputer } from "../testing/pairing";

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
