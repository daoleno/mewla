import { describe, expect, open, test } from "nsh/test";

/**
 * Owner 11-13 12:50, Extensions on the phone: a pasted code often did not take, or vanished by itself; the
 * placeholder moved; buttons often did nothing. A field drawn anew loses its text and focus, and a button
 * drawn anew under the finger loses its press. The computer sends Extensions news often (a sign-in is polled
 * every 3 s, `connections_changed` on any account change): what the person is typing in stays through it.
 */
const HARNESS = "../testing/Harness.tsx";
type Request = { action: string; [key: string]: unknown };

const catalog = [
  { id: "notion", name: "Notion", available: true, setup_url: "", description: "Pages and databases" },
  { id: "gws", name: "Google Workspace", available: true, setup_url: "", description: "Mail, Calendar, Drive" },
  { id: "mcp", name: "MCP server", available: true, setup_url: "", description: "Any MCP server" },
];
const notion = (n: number) => ({ id: `n${n}`, integration: "notion", name: `Notion ${n}`, identity: `me${n}@example.com`, enabled: true, status: "connected", history: [], access: { read: "allowed", write: "off" } });
const expires = () => new Date(Date.now() + 3_600_000).toISOString();
const setup = { id: "gws-flow", integration: "gws", status: "waiting", authorization_url: "https://console.cloud.google.com/auth/overview", code_required: true, setup_required: true, message: "Set up the consent screen.", expires: expires() };

/** `answer` gives the connections, or `{ error }` for a refusal; `delay` is how long the computer takes to answer. */
function computer(page: unknown, answer: (request: Request, n: number) => Record<string, unknown>, delay = 0) {
  const asked: Request[] = [];
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
          "app.daemon": async ({ type, body }: { type: string; body: Record<string, unknown> }) => {
            if (type !== "connections") return { error: "not in this test" };
            const request = body.connection_request as Request;
            asked.push(request);
            const reply = answer(request, asked.filter((r) => r.action === request.action).length);
            if (delay && request.action !== "list") await new Promise((done) => setTimeout(done, delay));
            return typeof reply.error === "string" ? reply : { reply: "connections_result", connections: reply };
          },
        },
      },
    },
  };
}

/** What the computer sends while the person types: accounts changing, and the sign-in's polls (each a new message). */
async function news(app: Awaited<ReturnType<typeof open>>) {
  for (let i = 1; i <= 3; i += 1) {
    await app.host.send("app.daemonPush", { type: "connections_changed", connections: { accounts: Array.from({ length: i }, (_, n) => notion(n)) } });
    await app.wait(100);
  }
  await app.wait(3_300);
}

describe("Extensions keeps what is being typed through the computer's news", () => {
  for (const page of [{ name: "extensions" }, { name: "extensions-service", id: "gws" }]) {
    test(`a pasted client on the sign-in card (${page.name})`, async () => {
      const c = computer(page, (request, n) => {
        if (request.action === "list") return { catalog, accounts: [] };
        // Each poll says something new about the same sign-in, as the computer's checks do.
        return { flow: { ...setup, message: `Set up the consent screen. (${n})` } };
      });
      const app = await open(HARNESS, c.options);
      await app.waitFor(page.name === "extensions" ? "id:connect-gws" : "id:service-connect");
      await app.press(page.name === "extensions" ? "id:connect-gws" : "id:service-connect");
      await app.waitFor("id:sign-in-code");
      await app.paste("id:sign-in-code", { text: "1234-abcd.apps.googleusercontent.com" });
      expect(app.value("id:sign-in-code")).toBe("1234-abcd.apps.googleusercontent.com");
      await news(app);
      expect(c.asked.filter((r) => r.action === "connect_status").length).toBeGreaterThan(0);
      expect(app.value("id:sign-in-code")).toBe("1234-abcd.apps.googleusercontent.com");
      expect(app.enabled("id:sign-in-code-continue")).toBe(true);
    }, 30_000);
  }

  test("a custom service's form", async () => {
    const c = computer({ name: "extensions-service", id: "mcp" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:custom-endpoint");
    await app.type("id:custom-name", "Docs");
    await app.paste("id:custom-endpoint", { text: "https://docs.example.com/mcp" });
    await news(app);
    expect(app.value("id:custom-name")).toBe("Docs");
    expect(app.value("id:custom-endpoint")).toBe("https://docs.example.com/mcp");
  }, 30_000);

  test("the search on Extensions", async () => {
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:extensions-search");
    await app.type("id:extensions-search", "goo");
    await news(app);
    expect(app.value("id:extensions-search")).toBe("goo");
  }, 30_000);
});

/**
 * A tap that spans the computer's news: the finger lands, news comes, the finger lifts. A button made again
 * under the finger never hears the lift, so the tap did nothing (owner 11-13: "buttons there often do nothing").
 */
async function tapAcrossNews(app: Awaited<ReturnType<typeof open>>, selector: string) {
  await app.dragAndHold(selector, { x: 0, y: 0 });
  // News that moves nothing on screen: the listed account's identity changed.
  await app.host.send("app.daemonPush", { type: "connections_changed", connections: { accounts: [{ ...notion(0), identity: "new@example.com" }] } });
  await app.drop();
}

describe("Extensions' buttons take a tap that spans the computer's news", () => {
  test("a service's Connect in the list", async () => {
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [notion(0)] } : { flow: { ...setup } }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-gws");
    await tapAcrossNews(app, "id:connect-gws");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_start"));
  }, 20_000);

  test("Connect on a service's page", async () => {
    const c = computer({ name: "extensions-service", id: "gws" }, (request) => (request.action === "list" ? { catalog, accounts: [notion(0)] } : { flow: { ...setup } }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:service-connect");
    await tapAcrossNews(app, "id:service-connect");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_start"));
  }, 20_000);

  test("Continue on the sign-in card", async () => {
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [notion(0)] } : { flow: { ...setup } }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-gws");
    await app.press("id:connect-gws");
    await app.waitFor("id:sign-in-code");
    await app.paste("id:sign-in-code", { text: "1234-abcd.apps.googleusercontent.com" });
    await tapAcrossNews(app, "id:sign-in-code-continue");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_finish"));
  }, 20_000);

  test("Connect on a custom service's form", async () => {
    const c = computer({ name: "extensions-service", id: "mcp" }, (request) => (request.action === "list" ? { catalog, accounts: [notion(0)] } : { flow: { ...setup, integration: "mcp" } }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:custom-endpoint");
    await app.type("id:custom-endpoint", "https://docs.example.com/mcp");
    await tapAcrossNews(app, "id:custom-connect");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_start"));
  }, 20_000);
});

describe("a tapped button shows that it took", () => {
  test("Connect: busy at once while the computer answers, then what it said in plain words", async () => {
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : { error: "The computer could not start this sign-in.", code: "plugin_request_failed" }), 1_500);
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-notion");
    await app.press("id:connect-notion");
    // While the computer answers: the button is a spinner, and every other service waits.
    await app.waitFor("label:Waiting");
    expect(app.shows("id:connect-notion")).toBe(false);
    expect(app.enabled("id:connect-gws")).toBe(false);
    await app.waitFor("text:That didn't go through");
    expect(app.texts()).toContain("The computer could not start this sign-in.");
    // And the row can be tapped again.
    await app.waitUntil(() => app.enabled("id:connect-notion"));
  }, 20_000);
});
