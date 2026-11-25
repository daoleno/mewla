import { describe, expect, open, test } from "nsh/test";
import { answering, pressWhenEnabled } from "../../testing/fixtures";

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
    await app.waitFor("text:Available");
    for (const word of ["Notion", "GitHub", "Unavailable", "MCP server"]) expect(app.texts()).toContain(word);

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
    // Connected says so, and offers the one next step: changes are off until allowed.
    expect(app.texts()).toContain("Allow changes");
    expect(c.secure.has("mewla.plugin.authorization.s1")).toBe(false);
  });

  test("grouped by state (SPEC §10.4): Connected rows say their state plainly, a sign-in to redo sits under Needs fixing with Reconnect", async () => {
    const github = { ...notionAccount, id: "g1", integration: "github", name: "octocat", status: "authorization_required" };
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog, accounts: [notionAccount, github] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Needs fixing");
    // Under Connected the section says it: the row repeats neither its state nor the service's name.
    expect(app.shows("id:service-notion-state")).toBe(false);
    expect(app.shows("id:connect-github")).toBe(true);
    expect(app.texts()).toContain("Reconnect");
    // The section says what is wrong: its one account isn't named again, nor "Sign-in expired".
    expect(app.texts().some((text) => text.includes("Sign-in expired"))).toBe(false);
    // Its page: the mark on the hero and the state with its dot.
    await app.press("id:service-notion");
    await app.waitFor("id:service-hero-lead");
    expect(app.shows("id:service-state-dot")).toBe(true);
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
    const app = await open(HARNESS, { ...c.options, clock: {} });
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
    expect(app.texts()).toContain("Allow changes");
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
  test("its account's switches tell the computer, and Disconnect asks once, by name", async () => {
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
      expect(dialog.buttons).toEqual(["Cancel", "Disconnect"]);
      return "Disconnect";
    });
    // The switch's change is still being saved: Disconnect waits for it.
    await answering(app, () => pressWhenEnabled(app, "id:disconnect-n1"));
    await app.waitUntil(() => c.asked.some((r) => r.action === "disconnect"));
    await app.waitFor("id:service-connect");
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
    expect(app.texts()).toContain("Choose tools");
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
    await app.waitUntil(() => !app.shows("label:Loading tools"));
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
    const c = computer({ name: "extensions-service", id: "mcp" }, (request) => (request.action === "list" ? { catalog, accounts: [] } : {}));
    const app = await open(HARNESS, { host: { ...c.options.host, params: { ...c.options.host.params, server: { name: "192.168.1.5" } } } });
    await app.waitFor("text:Credentials stay on this computer. Each tool needs your permission.");
    await app.host.send("app.serverRenamed", "claude9983");
    await app.waitFor("text:Credentials stay on claude9983. Each tool needs your permission.");
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
    await app.waitFor("label:Loading services");
    await app.waitFor("id:services-unavailable", { timeout: 4_000 });
    expect(app.texts()).toContain("Can't reach your computer.");
  });
});

describe("Extensions on the web, as on the phone (owner 11-13)", () => {
  /** A computer whose list comes late: a reloaded or shared /extensions/<id> opens the page before it. */
  function slowList(page: unknown, accounts: unknown[] = []) {
    const secure = new Map<string, string>();
    return {
      secure,
      options: {
        host: {
          params: { page },
          handlers: {
            ...secureStore(secure),
            "app.daemon": async ({ type, body }: { type: string; body: Record<string, unknown> }) => {
              if (type !== "connections") return { error: "not in this test" };
              const request = body.connection_request as Request;
              if (request.action === "list") await new Promise((done) => setTimeout(done, 400));
              return { reply: "connections_result", connections: request.action === "list" ? { catalog, accounts } : {} };
            },
          },
        },
      },
    };
  }

  test("a service page opened before the list names its service and shows its mark once the list comes", async () => {
    const c = slowList({ name: "extensions-service", id: "notion" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:service-connect");
    await app.waitFor("id:service-hero-lead");
    expect(app.texts()).toContain("Notion");
    expect(app.texts()).not.toContain("Service");
  });

  test("a sign-in under way shows its card on Extensions and on its own service's page, not over another service's", async () => {
    const flow = { id: "g1", integration: "github", status: "waiting", user_code: "WDJB-MJHT", authorization_url: "https://github.com/login/device", expires: expires() };
    for (const [page, card] of [[{ name: "extensions" }, true], [{ name: "extensions-service", id: "github" }, true], [{ name: "extensions-service", id: "notion" }, false]] as const) {
      const c = computer(page, (request) => (request.action === "list" ? { catalog, accounts: [] } : { flow }));
      c.secure.set("mewla.plugin.authorization.s1", JSON.stringify({ serverId: "s1", flow }));
      const app = await open(HARNESS, c.options);
      await app.waitUntil(() => c.asked.some((r) => r.action === "connect_status"));
      await app.wait(200);
      expect(app.shows("id:device-code")).toBe(card);
      // While it is under way, its own page doesn't offer a second Connect beside the card.
      if (page.name === "extensions-service" && page.id === "github") expect(app.shows("id:service-connect")).toBe(false);
    }
  });
});

describe("a sign-in card says what to do in one line", () => {
  test("a Google client to paste: the console's steps wait behind Steps, the field and Continue are there", async () => {
    const setup = { id: "w1", integration: "gws", status: "waiting", authorization_url: "https://console.cloud.google.com/auth/overview", code_required: true, setup_required: true, message: "Create a Google Cloud project, then paste its client ID and secret.", expires: expires() };
    const gwsCatalog = [...catalog, { id: "gws", name: "Google Workspace", available: true, setup_url: "", description: "Mail" }];
    const c = computer({ name: "extensions-service", id: "gws" }, (request) => (request.action === "list" ? { catalog: gwsCatalog, accounts: [] } : { flow: setup }));
    const app = await open(HARNESS, c.options);
    await app.press("id:service-connect");
    await app.waitFor("text:Paste the client ID and secret");
    expect(app.texts()).not.toContain(setup.message);
    expect(app.shows("id:sign-in-code")).toBe(true);
    expect(app.texts()).toContain("Open console");
    await app.press("id:sign-in-steps");
    await app.waitFor(`text:${setup.message}`);
    // × stops the sign-in.
    await app.press("id:close-sign-in");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_cancel"));
  });
});

describe("Needs fixing reads at a glance (owner 11-14)", () => {
  const services = [
    { id: "github", name: "GitHub", available: true, setup_url: "", description: "Repositories" },
    { id: "aws", name: "AWS", available: true, setup_url: "", description: "Cloud" },
    { id: "notion", name: "Notion", available: true, setup_url: "", description: "Pages" },
  ];
  const account = (id: string, integration: string, name: string, status: string, extra: Record<string, unknown> = {}) =>
    ({ id, integration, name, identity: name, enabled: true, status, history: [], access: { read: "allowed", write: "off" }, ...extra });
  const accounts = [
    account("g1", "github", "daoleno", "authorization_required"),
    account("g2", "github", "work-account", "connected"),
    ...["ace-mgmt", "old-1", "old-2"].map((name) => account(`a-${name}`, "aws", name, "authorization_required", { profile: name, profile_status: "expired", auth_method: "cli" })),
    account("a-prod", "aws", "freeride-prod", "connected", { profile: "freeride-prod", profile_status: "valid", auth_method: "cli" }),
  ];

  test("each row: the name, which account only when it tells it apart, one enabled button and no chevron", async () => {
    const waiting = { id: "n1", integration: "notion", status: "waiting", authorization_url: "https://notion.example/authorize?state=s", expires: expires() };
    const c = computer({ name: "extensions" }, (request) => {
      if (request.action === "list") return { catalog: services, accounts };
      if (request.action === "connect_status") return { flow: waiting };
      if (request.action === "connect_cancel") return { flow: { ...waiting, status: "cancelled" } };
      return { flow: { id: "g-flow", integration: "github", status: "waiting", user_code: "ABCD-1234", authorization_url: "https://github.com/login/device", expires: expires() } };
    });
    // A Notion sign-in left waiting: it used to grey out every Reconnect.
    c.secure.set("mewla.plugin.authorization.s1", JSON.stringify({ serverId: "s1", flow: waiting }));
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Needs fixing");
    const texts = app.texts();
    expect(texts.some((text) => text.includes("Sign-in expired"))).toBe(false);
    // GitHub: one of two needs it, so it is named; the working one is not.
    expect(texts).toContain("daoleno");
    expect(texts).not.toContain("work-account");
    expect(texts).not.toContain("daoleno · work-account");
    // AWS: three of four.
    expect(texts).toContain("3 of 4");
    expect(app.enabled("id:connect-github")).toBe(true);
    expect(app.enabled("id:connect-aws")).toBe(true);
    expect(texts).toContain("Reconnect");
    expect(texts).toContain("Fix");
    // GitHub's Reconnect starts its sign-in (ending the waiting one); AWS's Fix opens its page, each account there.
    await app.press("id:connect-github");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_start"));
    expect((c.asked.find((r) => r.action === "connect_start")!.input as { integration: string }).integration).toBe("github");
  });

  test("Fix on a service with several accounts to sign in again opens its page", async () => {
    const c = computer({ name: "extensions" }, (request) => (request.action === "list" ? { catalog: services, accounts } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:connect-aws");
    await app.press("id:connect-aws");
    await app.waitFor("id:service-page");
    expect(c.asked.some((r) => r.action === "connect_start")).toBe(false);
  });
});

describe("Slack sign-ins made before Mewla read files and DMs (11-19)", () => {
  const slackCatalog = [{ id: "slack", name: "Slack", available: true, setup_url: "", description: "Channels" }];
  const older = { id: "s1", integration: "slack", name: "FreeRide AI · paul", identity: "FreeRide AI · paul", enabled: true, status: "connected", history: [], access: { read: "allowed", write: "allowed", allowed: 6, tools: 6, read_consent: true } };

  test("sit under Needs fixing; Reconnect asks Slack again, keeping changes the account allowed", async () => {
    const c = computer({ name: "extensions" }, (request) => {
      if (request.action === "list") return { catalog: slackCatalog, accounts: [older] };
      if (request.action === "connect_start") return { flow: { id: "f1", integration: "slack", status: "waiting", authorization_url: "https://slack.com/oauth/v2/authorize?state=s", expires: expires() } };
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Needs fixing");
    expect(app.texts()).toContain("Reconnect");
    await app.press("id:connect-slack");
    await app.waitUntil(() => c.asked.some((r) => r.action === "connect_start"));
    // It names the card, so the computer renews that account in place (same ID).
    expect(c.asked.find((r) => r.action === "connect_start")).toEqual({ action: "connect_start", input: { integration: "slack", account_id: "s1", allow_writes: true } });
  });

  test("the account card says what Reconnect brings", async () => {
    const c = computer({ name: "extensions-service", id: "slack" }, (request) => (request.action === "list" ? { catalog: slackCatalog, accounts: [older] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:recover-s1");
    expect(app.texts()).toContain("Reconnect to read files and DMs");
  });
});

describe("a cloud card whose tool could not be installed says why (11-21)", () => {
  const awsCatalog = [{ id: "aws", name: "AWS", available: true, setup_url: "", description: "Cloud accounts" }];
  const card = { id: "aws:work", integration: "aws", name: "work", profile: "work", profile_kind: "SSO", profile_status: "unavailable", unavailable: "Could not install the CLI: dial tcp: no route to host", identity: "", enabled: true, status: "authorization_required", history: [] };
  test("the card shows the reason itself, and Check now asks the computer again", async () => {
    const c = computer({ name: "extensions-service", id: "aws" }, (request) => (request.action === "list" ? { catalog: awsCatalog, accounts: [card] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:unavailable-aws:work");
    expect(app.texts()).toContain("Unavailable");
    expect(app.texts()).toContain("Could not install the CLI: dial tcp: no route to host");
    await app.press("text:Check now");
    await app.waitUntil(() => c.asked.some((r) => r.action === "refresh"));
    expect(c.asked.find((r) => r.action === "refresh")).toEqual({ action: "refresh", id: "aws:work" });
  });
  test("a profile that is merely unchecked has no such row", async () => {
    const c = computer({ name: "extensions-service", id: "aws" }, (request) => (request.action === "list" ? { catalog: awsCatalog, accounts: [{ ...card, profile_status: "unchecked", unavailable: undefined }] } : {}));
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:enabled-aws:work");
    expect(app.texts()).not.toContain("Unavailable");
  });
});

describe("AWS accounts last as long as the organization allows (owner 11-17: AWS expiring daily)", () => {
  const awsCatalog = [{ id: "aws", name: "AWS", available: true, setup_url: "", description: "Cloud accounts" }];
  test("adding an account asks for the organization's access portal and shows the code to approve; aws login is the fallback", async () => {
    const c = computer({ name: "extensions-service", id: "aws" }, (request) => {
      if (request.action === "list") return { catalog: awsCatalog, accounts: [] };
      const input = request.input as Record<string, unknown> | undefined;
      if (request.action === "connect_start" && input?.sso_start_url) return { flow: { id: "p2", integration: "aws", status: "waiting", user_code: "QRST-WXYZ", authorization_url: "https://us-east-1.signin.aws/platform/authorize?request_uri=x", expires: expires() } };
      if (request.action === "connect_start" && input?.aws_login) return { flow: { id: "p3", integration: "aws", status: "waiting", code_required: true, authorization_url: "https://us-east-1.signin.aws.amazon.com/v1/authorize?state=s", expires: expires() } };
      if (request.action === "connect_start") return { flow: { id: "p1", integration: "aws", status: "waiting", portal_required: true, expires: expires() } };
      if (request.action === "connect_status" && request.flow_id === "p3") return { flow: { id: "p3", integration: "aws", status: "waiting", code_required: true, authorization_url: "https://us-east-1.signin.aws.amazon.com/v1/authorize?state=s", expires: expires() } };
      if (request.action === "connect_status") return { flow: { id: request.flow_id, integration: "aws", status: "waiting", user_code: "QRST-WXYZ", expires: expires() } };
      return { flow: { id: request.flow_id, integration: "aws", status: "cancelled", expires: expires() } };
    });
    const app = await open(HARNESS, c.options);
    await pressWhenEnabled(app, "id:service-connect");
    await app.waitFor("id:aws-portal-card");
    expect(app.shows("id:aws-portal-region")).toBe(false);
    await app.type("id:aws-portal-url", "https://d-1234567890.awsapps.com/start");
    // A d-… portal does not name its Region: it is asked for.
    await app.waitFor("id:aws-portal-region");
    await app.type("id:aws-portal-region", "eu-west-1");
    await pressWhenEnabled(app, "id:aws-portal-continue");
    await app.waitFor("id:device-code");
    expect(app.texts()).toContain("QRST-WXYZ");
    expect(c.asked.find((r) => r.action === "connect_start" && (r.input as Record<string, unknown>)?.sso_start_url)?.input).toEqual({ integration: "aws", sso_start_url: "https://d-1234567890.awsapps.com/start", sso_region: "eu-west-1" });
    // Nothing is pasted back: the card has no field.
    expect(app.shows("id:sign-in-code")).toBe(false);

    await app.press("id:close-sign-in");
    await pressWhenEnabled(app, "id:service-connect");
    await app.waitFor("id:aws-login-instead");
    await app.press("id:aws-login-instead");
    await app.waitFor("id:sign-in-code");
    expect(c.asked.filter((r) => r.action === "connect_start").at(-1)?.input).toEqual({ integration: "aws", aws_login: true });
  });

  test("an Identity Center profile shows when its sign-in really ends, and its length is set once from Identity Center's choices", async () => {
    const at = new Date(Date.now() - 3_600_000).toISOString();
    const profile = { id: "aws:prod", integration: "aws", name: "prod", profile: "prod", profile_kind: "SSO", profile_status: "valid", profile_signed_in_at: at, identity: "", enabled: true, status: "connected", history: [] };
    let length = 0;
    const c = computer({ name: "extensions-service", id: "aws" }, (request) => {
      if (request.action === "list") return { catalog: awsCatalog, accounts: [length ? { ...profile, profile_session_length_s: length, profile_sign_in_by: new Date(Date.parse(at) + length * 1000).toISOString(), profile_sign_in_limit: "session" } : profile] };
      if (request.action === "cloud_session_length") {
        length = request.seconds as number;
        return { account: { ...profile, profile_session_length_s: length, profile_sign_in_by: new Date(Date.parse(at) + length * 1000).toISOString(), profile_sign_in_limit: "session" } };
      }
      return {};
    });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:session-length-aws:prod");
    // Not known yet: no end is invented (no "Up to 89d" from the client registration).
    expect(app.texts().some((text) => /left|Ends in/.test(text))).toBe(false);
    expect(app.texts()).toContain("Not known");
    app.onDialog((dialog) => {
      expect(dialog.title).toBe("As set in IAM Identity Center");
      expect(dialog.buttons).toContain("90 days");
      return "90 days";
    });
    await answering(app, () => app.press("id:session-length-aws:prod"));
    await app.waitUntil(() => c.asked.some((r) => r.action === "cloud_session_length"));
    expect(c.asked.find((r) => r.action === "cloud_session_length")).toEqual({ action: "cloud_session_length", id: "aws:prod", seconds: 7776000 });
    await app.waitFor("text:90 days");
    await app.waitUntil(() => app.texts().some((text) => text.includes("Ends in 89d")));
  });
});
