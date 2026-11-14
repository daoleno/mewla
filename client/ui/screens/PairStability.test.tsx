import { describe, expect, open, test } from "nsh/test";

/**
 * Pair a computer keeps a pasted link through the news a phone gets while the page is open (its computer's
 * connection drops and comes back, a pairing says what it is doing), and Pair held across it still takes:
 * a link that is not one says so in plain words. The same check as Extensions' (more/extensions/Stability.test.tsx).
 */
const HARNESS = "./testing/PairHarness.tsx";
type App = Awaited<ReturnType<typeof open>>;
const secure = () => {
  const store = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => store.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void store.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void store.delete(key),
    "scan.permission": () => "denied",
    "scan.request": () => "denied",
  };
};

async function news(app: App) {
  for (let i = 0; i < 3; i += 1) {
    await app.host.send("app.daemonState", "connecting");
    await app.host.send("app.pairingStatus", `Connecting through Mewla… (${i})`);
    await app.wait(50);
    await app.host.send("app.daemonState", "connected");
    await app.host.send("app.pairingStatus", "");
    await app.wait(300);
  }
}

describe("Pair a computer keeps what is pasted through the phone's news", () => {
  test("a pasted link stays, and Pair held across the news says it is not a code", async () => {
    const app = await open(HARNESS, { platform: "android", host: { handlers: secure() } });
    await app.waitFor("id:pair-link");
    await app.paste("id:pair-link", { text: "mewla://settings?p=not-a-code" });
    await news(app);
    expect(app.value("id:pair-link")).toBe("mewla://settings?p=not-a-code");
    await app.dragAndHold("id:pair-link-submit", { x: 0, y: 0 });
    await news(app);
    await app.drop();
    await app.waitFor("text:Not a pairing code");
    // A link that did not pair stays, so it can be fixed.
    expect(app.value("id:pair-link")).toBe("mewla://settings?p=not-a-code");
  }, 30_000);
});
