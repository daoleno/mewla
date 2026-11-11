import { describe, expect, open, test } from "nsh/test";
import { answering } from "../../testing/fixtures";

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

describe("Extensions in a browser", () => {
  const web = { platform: "web" as const, width: 1280, height: 800 };
  /** The page as web/host.js serves it: its origin, a sign-in in this tab, and how the last one came back. */
  function browser(c: ReturnType<typeof computer>, returned: unknown = null) {
    const here: unknown[] = [];
    const options = {
      ...c.options,
      ...web,
      host: {
        ...c.options.host,
        handlers: {
          ...c.options.host.handlers,
          "app.origin": "https://desk.example",
          "links.pluginReturn": returned,
          "links.openHere": (data: unknown) => void here.push(data),
        },
      },
    };
    return { options, here };
  }

  test("a sign-in names the page's origin and takes this tab to the provider", async () => {
    const flow = { id: "f1", integration: "notion", status: "waiting", authorization_url: "https://notion.example/authorize?state=s123", expires: expires(), web_return: true };
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : { flow }));
    const b = browser(c);
    const app = await open(HARNESS, b.options);
    await app.waitFor("id:connect-notion");
    await app.press("id:connect-notion");
    await app.waitUntil(() => b.here.length === 1);
    expect(b.here).toEqual([{ url: "https://notion.example/authorize?state=s123" }]);
    expect(app.openedUrls()).toEqual([]);
    expect(c.asked.find((r) => r.action === "connect_start")).toEqual({ action: "connect_start", input: { integration: "notion", allow_writes: false, web_origin: "https://desk.example" } });
    expect([...c.secure.keys()]).toContain("mewla.plugin.authorization.s1");
  });

  test("a code sign-in opens its page in a tab of its own", async () => {
    const flow = { id: "g1", integration: "github", status: "waiting", user_code: "WDJB-MJHT", authorization_url: "https://github.com/login/device", expires: expires() };
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : { flow }));
    const b = browser(c);
    const app = await open(HARNESS, b.options);
    await app.waitFor("id:connect-github");
    await app.press("id:connect-github");
    await app.waitFor("id:device-code");
    await app.press("id:open-code-page");
    await app.waitUntil(() => app.openedUrls().length === 1);
    expect(app.openedUrls()).toEqual(["https://github.com/login/device"]);
    expect(b.here).toEqual([]);
  });

  test("a page the provider sent back to /plugins?connected= shows the account connected and forgets the sign-in", async () => {
    const flow = { id: "f1", integration: "notion", status: "waiting", authorization_url: "https://notion.example/authorize?state=s123", expires: expires(), web_return: true };
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [notionAccount] } : { flow }));
    c.secure.set("mewla.plugin.authorization.s1", JSON.stringify({ serverId: "s1", flow }));
    const b = browser(c, { connected: "n1", service: "notion" });
    const app = await open(HARNESS, b.options);
    await app.waitFor("text:Notion connected");
    expect(app.texts()).toContain("Brain can now find and read workspace pages.");
    expect(c.secure.has("mewla.plugin.authorization.s1")).toBe(false);
    expect(c.asked.some((r) => r.action === "connect_status")).toBe(false);
  });

  test("a page sent back with plugin_error says which step failed and can start again", async () => {
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const b = browser(c, { plugin_error: "denied", service: "notion" });
    const app = await open(HARNESS, b.options);
    await app.waitFor("id:connect-failed");
    expect(app.texts()).toContain("Couldn't connect Notion");
    expect(app.texts()).toContain("Access was not granted. You can connect again whenever you’re ready.");
    expect(app.texts()).not.toContain("That sign-in already ended");
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
    await answering(app, () => app.press("id:disconnect-n1"));
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

  test("without an account to show, the page stops loading", async () => {
    const c = computer({ name: "extensions-tools", service: "notion" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitUntil(() => c.asked.some((r) => r.action === "list"));
    await app.waitUntil(() => !app.shows("text:Loading tools"));
  });
});

const more = [
  ...catalog,
  { id: "aws", name: "AWS", available: true, setup_url: "", description: "Cloud accounts" },
  { id: "hyperliquid", name: "Hyperliquid", available: true, setup_url: "", description: "Markets" },
];
const account = (id: string, overrides: Record<string, unknown> = {}) => ({ ...notionAccount, id, name: id, identity: `${id}@example.com`, ...overrides });

describe("As before the rewrite", () => {
  test("after an AWS sign-in, the card offers a readable profile name and saves it", async () => {
    const aws = { id: "aws:mewla-temp", integration: "aws", name: "mewla-temp", profile: "mewla-temp", identity: "", enabled: true, status: "connected", history: [], aws: { account_id: "123456789012", account_name: "ACE Labs Mgmt", who: "Admin" }, access: { read: "allowed", write: "off" } };
    const c = computer({ name: "extensions" }, (request) => {
      if (request.action === "list") return { catalog: more, accounts: [] };
      if (request.action === "connect_start") return { flow: { id: "a1", integration: "aws", status: "connected", expires: expires() }, account: aws };
      if (request.action === "cloud_rename") return { account: { ...aws, id: `aws:${request.profile}`, name: request.profile, profile: request.profile } };
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-aws");
    await app.press("id:connect-aws");
    await app.waitFor("id:profile-name");
    expect(app.value("id:profile-name-field")).toBe("ace-labs-mgmt-admin");
    await app.press("id:profile-name-save");
    await app.waitUntil(() => c.asked.some((r) => r.action === "cloud_rename"));
    expect(c.asked.find((r) => r.action === "cloud_rename")).toEqual({ action: "cloud_rename", id: "aws:mewla-temp", profile: "ace-labs-mgmt-admin" });
    await app.waitUntil(() => !app.shows("id:connected-card"));
  });

  test("a custom service's form says where its credentials stay", async () => {
    const c = computer({ name: "extensions-service", id: "mcp" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Credentials stay on Desk. Each tool needs your permission.");
  });

  // FINAL-VERIFY 6387f88f extensions D4: a computer paired by its address took its reported name in
  // Settings, but the pages kept saying "this computer" until the app started again.
  test("a computer that takes its reported name is named by it at once", async () => {
    const c = computer({ name: "extensions-service", id: "github" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const app = await open(HARNESS, { host: { ...c.options.host, params: { ...c.options.host.params, server: { name: "192.168.1.5" } } } });
    await app.waitFor("text:Your sign-in stays on this computer. Brain only acts on tasks you give it.");
    await app.host.send("app.serverRenamed", "claude9983");
    await app.waitFor("text:Your sign-in stays on claude9983. Brain only acts on tasks you give it.");
  });

  test("Hyperliquid offers another key only once an account holds one", async () => {
    const reader = { id: "h1", integration: "hyperliquid", name: "Mainnet", identity: "0xabc", enabled: true, status: "connected", history: [] };
    let accounts: unknown[] = [reader];
    const c = computer({ name: "extensions-service", id: "hyperliquid" }, (request) => (request.action === "list" ? { catalog: more, accounts } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Add API wallet key");
    accounts = [{ ...reader, signer: "0x1234567890abcdef" }];
    await app.host.send("app.daemonPush", { type: "connections_changed", connections: { accounts } });
    await app.waitFor("text:Add another key");
  });

  test("a service with more than two accounts counts them, and more than eight can be filtered", async () => {
    const many = Array.from({ length: 9 }, (_, i) => account(`acct-${i + 1}`));
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: many } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:9 accounts");
    await app.press("id:service-notion");
    await app.waitFor("id:filter-accounts");
    await app.type("id:filter-accounts", "acct-3");
    await app.waitUntil(() => !app.shows("id:account-acct-4"));
    expect(app.shows("id:account-acct-3")).toBe(true);
  });

  test("an AWS profile's time left counts down while the page stays open", async () => {
    const profile = { id: "aws:work", integration: "aws", name: "work", profile: "work", profile_status: "valid", profile_sign_in_by: new Date(Date.now() + 3 * 3_600_000 - 60_000).toISOString(), profile_sign_in_limit: "maximum", identity: "", enabled: true, status: "connected", history: [] };
    const c = computer({ name: "extensions-service", id: "aws" }, (request) => (request.action === "list" ? { catalog: more, accounts: [profile] } : {}));
    const app = await open(HARNESS, { ...c.options, clock: {} });
    await app.clock!.runFor(100);
    await app.waitFor("text:Up to 2h left");
    await app.clock!.fastForward(61 * 60_000);
    await app.waitFor("text:Up to 1h left");
  });

  test("a computer still out of reach after a moment says so, not at once", async () => {
    const c = computer({ name: "extensions" }, () => ({}));
    const options = { host: { ...c.options.host, params: { page: { name: "extensions" }, state: "waiting" } } };
    const app = await open(HARNESS, options);
    await app.waitFor("text:Loading services");
    await app.waitFor("id:services-unavailable", { timeout: 4_000 });
    expect(app.texts()).toContain("Can't reach your computer.");
  });
});
