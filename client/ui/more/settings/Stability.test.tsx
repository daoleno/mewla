import { describe, expect, open, test } from "nsh/test";
import { pressUntil } from "../../testing/fixtures";

/**
 * The same check as Extensions' (more/extensions/Stability.test.tsx) on Settings' fields: what the person types
 * or pastes stays through the computer's news, and a tap held across it still takes. On a phone the news here is
 * the connection itself: it drops and comes back (Wi-Fi, a tunnel), and each page reads the list again.
 */
const HARNESS = "../testing/Harness.tsx";
type Asked = { type: string; body: Record<string, unknown> };
type App = Awaited<ReturnType<typeof open>>;

function computer(page: unknown, answer: (type: string, body: Record<string, unknown>) => unknown) {
  const asked: Asked[] = [];
  const secure = new Map<string, string>();
  return {
    asked,
    options: {
      host: {
        params: { page },
        handlers: {
          "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
          "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
          "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
          "app.daemon": ({ type, body }: Asked) => {
            asked.push({ type, body });
            return answer(type, body) ?? { error: `unexpected ${type}` };
          },
        },
      },
    },
  };
}

/** The connection drops and comes back, three times. */
async function news(app: App) {
  for (let i = 0; i < 3; i += 1) {
    await app.host.send("app.daemonState", "connecting");
    await app.wait(50);
    await app.host.send("app.daemonState", "connected");
    await app.wait(300);
  }
}

const providers = { revision: 3, connections: [], defaults: { codex: { connection_id: "" } }, presets: [], models: {} };
const disabledBot = { connection: { state: "disabled", enabled: false, binding_pending: false } };

describe("Settings keeps what is being typed through the computer's news", () => {
  test("a new Provider's name, address and pasted key", async () => {
    const c = computer({ name: "settings-providers" }, (type) => (type === "list_providers" ? providers : null));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-add");
    await pressUntil(app, "id:provider-add", "id:provider-name");
    await app.type("id:provider-name", "Home");
    await app.paste("id:provider-url", { text: "https://c9.example.com/v1" });
    await app.paste("id:provider-key", { text: "sk-home-1234" });
    const reads = c.asked.filter((a) => a.type === "list_providers").length;
    await news(app);
    expect(c.asked.filter((a) => a.type === "list_providers").length).toBeGreaterThan(reads);
    expect(app.value("id:provider-name")).toBe("Home");
    expect(app.value("id:provider-url")).toBe("https://c9.example.com/v1");
    expect(app.value("id:provider-key")).toBe("sk-home-1234");
  }, 30_000);

  test("a pasted Telegram token", async () => {
    const c = computer({ name: "settings-telegram" }, (type) => (type === "telegram_connection_status" ? disabledBot : null));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:telegram-token");
    await app.paste("id:telegram-token", { text: "123456:ABC-def" });
    const reads = c.asked.filter((a) => a.type === "telegram_connection_status").length;
    await news(app);
    expect(c.asked.filter((a) => a.type === "telegram_connection_status").length).toBeGreaterThan(reads);
    expect(app.value("id:telegram-token")).toBe("123456:ABC-def");
    // The token never stays on the phone: it goes when the app leaves the front.
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    await app.waitUntil(() => app.value("id:telegram-token") === "");
  }, 30_000);

  test("the computer's name and address", async () => {
    const c = computer({ name: "settings-computer", id: "s1" }, () => null);
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:computer-name");
    await app.clear("id:computer-name");
    await app.type("id:computer-name", "Desk");
    await app.clear("id:computer-address");
    await app.paste("id:computer-address", { text: "https://desk.example.com" });
    await news(app);
    expect(app.value("id:computer-name")).toBe("Desk");
    expect(app.value("id:computer-address")).toBe("https://desk.example.com");
  }, 30_000);
});

describe("a tap held across the computer's news still takes (Settings)", () => {
  test("Save on a new Provider", async () => {
    const c = computer({ name: "settings-providers" }, (type) => {
      if (type === "list_providers") return providers;
      if (type === "upsert_provider_connection") return { error: "not saved in this test" };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:provider-add");
    await pressUntil(app, "id:provider-add", "id:provider-name");
    await app.type("id:provider-name", "Home");
    await app.type("id:provider-url", "https://c9.example.com/v1");
    await app.type("id:provider-key", "sk-home");
    await app.dragAndHold("id:provider-save", { x: 0, y: 0 });
    await news(app);
    await app.drop();
    await app.waitUntil(() => c.asked.some((a) => a.type === "upsert_provider_connection"));
    // The failure is said, in plain words, on the form that still holds what was typed.
    await app.waitFor("id:provider-failure");
    expect(app.value("id:provider-name")).toBe("Home");
  }, 30_000);

  test("Verify on a Telegram token", async () => {
    const c = computer({ name: "settings-telegram" }, (type) => {
      if (type === "telegram_connection_status") return disabledBot;
      if (type === "telegram_connection_configure") return { error: "Telegram rejected the token: Unauthorized" };
      return null;
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:telegram-token");
    await app.paste("id:telegram-token", { text: "123:bad" });
    await app.waitFor("id:telegram-verify");
    await app.dragAndHold("id:telegram-verify", { x: 0, y: 0 });
    await news(app);
    await app.drop();
    await app.waitUntil(() => c.asked.some((a) => a.type === "telegram_connection_configure"));
    await app.waitFor("text:Telegram setup failed");
  }, 30_000);
});
