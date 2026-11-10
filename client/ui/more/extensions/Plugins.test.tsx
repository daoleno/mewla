import { describe, expect, open, test } from "nsh/test";

const HARNESS = "../testing/Harness.tsx";

type Request = { action: string; [key: string]: unknown };

function secureStore(secure: Map<string, string>) {
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const catalog = [
  { id: "notion", name: "Notion", available: true, setup_url: "", description: "Pages and databases" },
  { id: "github", name: "GitHub", available: true, setup_url: "", description: "Repositories" },
  { id: "slack", name: "Slack", available: false, unavailable_reason: "Sign-in is not set up on this server.", setup_url: "", description: "Channels" },
  { id: "mcp", name: "MCP server", available: true, setup_url: "", description: "Any MCP server" },
];

const notionAccount = {
  id: "n1",
  integration: "notion",
  name: "Notion",
  identity: "me@example.com",
  enabled: true,
  status: "connected",
  history: [],
  access: { read: "allowed", write: "off", allowed: 3, tools: 5 },
};

const expires = () => new Date(Date.now() + 3_600_000).toISOString();

/** A computer whose `connections` answers come from `answer`; every request is recorded. */
function computer(page: unknown, answer: (request: Request) => Record<string, unknown> | { error: string; code?: string }) {
  const asked: Request[] = [];
  const secure = new Map<string, string>();
  return {
    asked,
    secure,
    options: {
      host: {
        params: { page },
        handlers: {
          ...secureStore(secure),
          "app.daemon": ({ type, body }: { type: string; body: Record<string, unknown> }) => {
            if (type === "skills_inventory" || type === "plugins_inventory") return { error: "not in this test" };
            if (type !== "connections") return { error: `unexpected ${type}` };
            const request = body.connection_request as Request;
            asked.push(request);
            const reply = answer(request);
            return "error" in reply ? reply : { reply: "connections_result", connections: reply };
          },
        },
      },
    },
  };
}

describe("Extensions", () => {
  test("lists the computer's services, and a sign-in opens in the browser and finishes on the app's return link", async () => {
    const flow = { id: "f1", integration: "notion", status: "waiting", authorization_url: "https://notion.example/authorize?state=s123", expires: expires() };
    const c = computer({ name: "extensions" }, (request) => {
      if (request.action === "list") return { catalog, accounts: [] };
      if (request.action === "connect_start") return { flow };
      if (request.action === "connect_status") return { flow };
      if (request.action === "connect_finish") return { flow: { ...flow, status: "connected" }, account: notionAccount };
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Mewla plugins");
    for (const word of ["Notion", "GitHub", "Not yet available", "Your own services"]) expect(app.texts()).toContain(word);

    await app.press("id:connect-notion");
    await app.waitFor("text:Waiting for Notion");
    expect(app.openedUrls()).toEqual(["https://notion.example/authorize?state=s123"]);
    expect(c.asked.find((r) => r.action === "connect_start")).toEqual({ action: "connect_start", input: { integration: "notion", allow_writes: false } });
    expect([...c.secure.keys()]).toContain("mewla.plugin.authorization.s1");

    // A return for another sign-in is not this one's.
    await app.host.send("links.open", { url: "mewla://plugins?state=other&code=x" });
    await app.wait(50);
    expect(c.asked.some((r) => r.action === "connect_finish")).toBe(false);

    await app.host.send("links.open", { url: "mewla://plugins?state=s123&code=good" });
    await app.waitFor("text:Notion connected");
    expect(c.asked.find((r) => r.action === "connect_finish")).toEqual({ action: "connect_finish", flow_id: "f1", callback: "mewla://plugins?state=s123&code=good" });
    expect(app.texts()).toContain("Brain can now find and read workspace pages.");
    expect(c.secure.has("mewla.plugin.authorization.s1")).toBe(false);
  });

  test("a GitHub code is shown first, then copied as the page opens", async () => {
    const flow = { id: "g1", integration: "github", status: "waiting", user_code: "WDJB-MJHT", authorization_url: "https://github.com/login/device", expires: expires() };
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : { flow }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-github");
    await app.press("id:connect-github");
    await app.waitFor("id:device-code");
    expect(app.texts()).toContain("WDJB-MJHT");
    expect(app.openedUrls()).toEqual([]);
    await app.press("id:open-code-page");
    await app.waitUntil(() => app.openedUrls().length === 1);
    expect(app.openedUrls()).toEqual(["https://github.com/login/device"]);
  });

  test("a sign-in the computer no longer has ends with what it said, and can start again", async () => {
    const flow = { id: "f2", integration: "notion", status: "waiting", authorization_url: "https://notion.example/authorize?state=s9", expires: expires() };
    let gone = false;
    const c = computer({ name: "extensions" }, (request) => {
      if (request.action === "list") return { catalog, accounts: [] };
      if (request.action === "connect_status" && gone) return { error: "This sign-in expired. Start again.", code: "plugin_request_failed" };
      return { flow };
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-notion");
    await app.press("id:connect-notion");
    await app.waitFor("text:Waiting for Notion");
    gone = true;
    await app.wait(3_100);
    await app.waitFor("id:connect-failed");
    expect(app.texts()).toContain("Couldn't connect Notion");
    expect(app.texts()).toContain("This sign-in expired. Start again.");
  });
});

describe("A service", () => {
  test("its account's switches tell the computer, and Disconnect asks once with Keep", async () => {
    let accounts = [notionAccount];
    const c = computer({ name: "extensions-service", id: "notion" }, (request) => {
      if (request.action === "list") return { catalog, accounts };
      if (request.action === "permissions") return { account: { ...notionAccount, access: { ...notionAccount.access, write: request.allowed ? "allowed" : "off" } } };
      if (request.action === "disconnect") {
        accounts = [];
        return { accounts: [] };
      }
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:write-n1");
    await app.press("id:write-n1");
    await app.waitUntil(() => c.asked.some((r) => r.action === "permissions"));
    expect(c.asked.find((r) => r.action === "permissions")).toEqual({ action: "permissions", id: "n1", group: "write", allowed: true });

    app.onDialog((dialog) => {
      expect(dialog.title).toBe("Disconnect Notion?");
      expect(dialog.buttons).toEqual(["Keep", "Disconnect"]);
      return "Disconnect";
    });
    await app.press("id:disconnect-n1");
    await app.waitUntil(() => c.asked.some((r) => r.action === "disconnect"));
    await app.waitFor("id:service-connect");
    expect(app.texts()).toContain("What Brain can do");
  });

  test("a custom service with a token connects without a sign-in, sending its address and token once", async () => {
    const c = computer({ name: "extensions-service", id: "mcp" }, (request) => {
      if (request.action === "list") return { catalog, accounts: [] };
      if (request.action === "add") return { account: { id: "m1", integration: "mcp", name: "Docs", identity: "", endpoint: "https://docs.example.com/mcp", enabled: true, status: "connected", history: [], access: { read: "none", write: "none", allowed: 0, tools: 4 } } };
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:custom-endpoint");
    await app.type("id:custom-name", "Docs");
    await app.type("id:custom-endpoint", "https://docs.example.com/mcp");
    await app.press("id:custom-browser");
    await app.type("id:custom-token", "tok_123");
    await app.press("id:custom-connect");
    await app.waitFor("text:Docs connected");
    expect(c.asked.find((r) => r.action === "add")).toEqual({ action: "add", input: { integration: "mcp", name: "Docs", endpoint: "https://docs.example.com/mcp", credential: "tok_123" } });
    expect(app.texts()).toContain("Choose which of its tools Brain may use. None are allowed yet.");
  });
});

describe("Tools & activity", () => {
  test("a tool switch moves at once and goes back when the computer refuses", async () => {
    const tools = [
      { name: "search_pages", description: "Search the workspace", allowed: true, group: "read" },
      { name: "create_page", description: "Create a page", allowed: false, group: "write" },
    ];
    const c = computer({ name: "extensions-tools", service: "notion", account: "n1" }, (request) => {
      if (request.action === "list") return { catalog, accounts: [notionAccount] };
      if (request.action === "get") return { account: { ...notionAccount, tools, history: [{ at: "2026-11-11T10:00:00Z", tool: "search_pages", status: "ok" }] } };
      if (request.action === "policy") return { error: "Policy rejected." };
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:tool-create_page");
    expect(app.texts()).toContain("search_pages");
    await app.press("id:tool-create_page");
    await app.waitUntil(() => c.asked.some((r) => r.action === "policy"));
    expect(c.asked.find((r) => r.action === "policy")).toEqual({ action: "policy", id: "n1", tool: "create_page", allowed: true });
    await app.waitFor("id:plugins-error");
    expect(app.texts()).toContain("Policy rejected.");
  });
});
