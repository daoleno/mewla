import { describe, expect, test } from "bun:test";
import type { ConnectionRequest, ConnectionResponse, ConnectFlow } from "./connections";
import { ConnectionsFlow, type FlowDeps } from "./connectionsFlow";
import { pendingConnectionKey } from "./pluginOnboarding";

const HOUR = 3_600_000;

function harness(answer: (request: ConnectionRequest) => Promise<ConnectionResponse> | ConnectionResponse) {
  const stored = new Map<string, string>();
  const asked: ConnectionRequest[] = [];
  const opened: string[] = [];
  const copied: string[] = [];
  const polls: Array<() => void> = [];
  let now = Date.UTC(2026, 10, 11, 12);
  const deps: FlowDeps = {
    serverId: "server",
    request: async (request) => {
      asked.push(request);
      return answer(request);
    },
    store: {
      get: async (key) => stored.get(key) ?? null,
      set: async (key, value) => void stored.set(key, value),
      remove: async (key) => void stored.delete(key),
    },
    openUrl: (url) => void opened.push(url),
    copy: (text) => void copied.push(text),
    now: () => now,
    every: (_ms, run) => {
      polls.push(run);
      return () => polls.splice(polls.indexOf(run), 1);
    },
    active: () => true,
  };
  const flow = new ConnectionsFlow(deps);
  return { flow, stored, asked, opened, copied, polls, advance: (ms: number) => (now += ms), deps };
}

const expires = () => new Date(Date.UTC(2026, 10, 11, 12) + HOUR).toISOString();
const oauth = (overrides: Partial<ConnectFlow> = {}): ConnectFlow => ({
  id: "f1",
  integration: "notion",
  status: "waiting",
  authorization_url: "https://notion.example/authorize?state=abc123&client_id=x",
  expires: expires(),
  ...overrides,
});
const coded = (message: string, code: string) => Object.assign(new Error(message), { code });

describe("connecting a service", () => {
  test("a sign-in opens in the system browser and finishes on the app's return link for exactly its state", async () => {
    const h = harness((request) => {
      if (request.action === "connect_start") return { flow: oauth() };
      if (request.action === "connect_finish") return { flow: oauth({ status: "connected" }), account: { id: "a1", integration: "notion", name: "Notion", identity: "me", enabled: true, status: "connected", history: [] } };
      return { flow: oauth() };
    });
    await h.flow.connect("notion");
    expect(h.opened).toEqual(["https://notion.example/authorize?state=abc123&client_id=x"]);
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.stored.has(pendingConnectionKey("server"))).toBe(true);
    expect(h.polls).toHaveLength(1);

    expect(h.flow.handleLink("mewla://plugins?state=other&code=1")).toBe(false);
    expect(h.asked.some((r) => r.action === "connect_finish")).toBe(false);

    expect(h.flow.handleLink("mewla://plugins?state=abc123&code=good")).toBe(true);
    await Bun.sleep(0);
    await Bun.sleep(0);
    expect(h.asked.at(-1)).toEqual({ action: "connect_finish", flow_id: "f1", callback: "mewla://plugins?state=abc123&code=good" });
    expect(h.flow.state.phase).toBe("connected");
    expect(h.flow.state.connectedId).toBe("a1");
    expect(h.flow.state.flow).toBeNull();
    expect(h.stored.size).toBe(0);
    expect(h.polls).toHaveLength(0);
  });

  test("Open again tapped twice while the browser opens opens it once", async () => {
    const h = harness(() => ({ flow: oauth({ integration: "github", user_code: "ABCD-1234", authorization_url: "https://github.com/login/device" }) }));
    await h.flow.connect("github");
    let release!: () => void;
    h.deps.openUrl = (url) => new Promise<void>((resolve) => { h.opened.push(url); release = resolve; });
    const first = h.flow.openBrowser();
    await Bun.sleep(0);
    const second = h.flow.openBrowser();
    release();
    await Promise.all([first, second]);
    expect(h.opened).toEqual(["https://github.com/login/device"]);
    // Once open, it can be opened again.
    h.deps.openUrl = (url) => void h.opened.push(url);
    await h.flow.openBrowser();
    expect(h.opened).toHaveLength(2);
  });

  test("a sign-in page the browser refused to open is remembered, and Open clears it once it opens", async () => {
    // AWS gives its page seconds after the tap: a browser no longer counts that as the person's and blocks the tab.
    const h = harness(() => ({ flow: oauth({ integration: "aws", code_required: true, authorization_url: "https://signin.aws.amazon.com/oauth?x=1" }) }));
    h.deps.openUrl = (url) => { h.opened.push(url); return Promise.resolve(false); };
    await h.flow.connect("aws");
    expect(h.opened).toEqual(["https://signin.aws.amazon.com/oauth?x=1"]);
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.flow.state.blocked).toBe(true);
    // The card's Open is a tap of its own: the browser lets it through.
    h.deps.openUrl = (url) => { h.opened.push(url); return Promise.resolve(true); };
    await h.flow.openBrowser();
    expect(h.flow.state.blocked).toBe(false);
    expect(h.opened).toHaveLength(2);
  });

  test("a GitHub code is shown and copied before the page opens, and the page is polled", async () => {
    const h = harness(() => ({ flow: oauth({ integration: "github", user_code: "ABCD-1234", authorization_url: "https://github.com/login/device" }) }));
    await h.flow.connect("github");
    expect(h.opened).toEqual([]);
    expect(h.flow.state.phase).toBe("waiting");
    await h.flow.openBrowser();
    expect(h.copied).toEqual(["ABCD-1234"]);
    expect(h.opened).toEqual(["https://github.com/login/device"]);
    h.polls[0]();
    await Bun.sleep(0);
    expect(h.asked.filter((r) => r.action === "connect_status").length).toBeGreaterThanOrEqual(2);
  });

  test("a form service connects from its page, which brings the details", async () => {
    const h = harness(() => ({}));
    expect(await h.flow.connect("mcp")).toBe("form");
    expect(h.asked).toEqual([]);
  });

  test("a sign-in left waiting when the app closed is picked up for this computer only, and expires", async () => {
    const h = harness(() => ({ flow: oauth() }));
    h.stored.set(pendingConnectionKey("server"), JSON.stringify({ serverId: "server", flow: oauth() }));
    await h.flow.restore(null);
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.asked.at(-1)).toEqual({ action: "connect_status", flow_id: "f1" });

    h.advance(2 * HOUR);
    await h.flow.check();
    expect(h.flow.state.phase).toBe("failed");
    expect(h.flow.state.connectError).toBe("This sign-in expired. Start again.");
    expect(h.stored.size).toBe(0);

    const other = harness(() => ({}));
    other.stored.set(pendingConnectionKey("server"), JSON.stringify({ serverId: "elsewhere", flow: oauth() }));
    await other.flow.restore(null);
    expect(other.flow.state.flow).toBeNull();
    expect(other.asked).toEqual([]);
  });

  test("the return link the app was opened with finishes the restored sign-in", async () => {
    const h = harness((request) => (request.action === "connect_finish" ? { flow: oauth({ status: "failed", message: "Access was not granted." }) } : { flow: oauth() }));
    h.stored.set(pendingConnectionKey("server"), JSON.stringify({ serverId: "server", flow: oauth() }));
    await h.flow.restore("mewla://plugins?state=abc123&error=access_denied");
    expect(h.asked.map((r) => r.action)).toEqual(["connect_finish"]);
    expect(h.flow.state.phase).toBe("failed");
    expect(h.flow.state.connectError).toBe("Access was not granted.");
  });

  test("a bad address is retryable, polling keeps the flow, and Continue during polling is delivered once", async () => {
    const flow = oauth({ id: "aws-flow", integration: "aws", address_required: true, authorization_url: "https://example.com" });
    let next: ((request: ConnectionRequest) => Promise<ConnectionResponse>) | null = null;
    const h = harness((request) => {
      if (next) {
        const run = next;
        next = null;
        return run(request);
      }
      return { flow };
    });
    await h.flow.connect("aws");
    const message = "That is not the address this sign-in ended on. Copy it from the browser's address bar, whole.";
    next = async () => {
      throw coded(message, "plugin_request_failed");
    };
    await expect(h.flow.submitAddress("wrong-address")).rejects.toThrow(message);
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.flow.state.flow?.flow.id).toBe("aws-flow");
    expect(h.stored.size).toBe(1);
    await h.flow.check();
    expect(h.flow.state.phase).toBe("waiting");

    let resolve!: (value: ConnectionResponse) => void;
    next = () => new Promise((r) => (resolve = r));
    const checking = h.flow.check();
    const submitting = h.flow.submitAddress("http://127.0.0.1:9876/plugins/oauth/callback?state=right&code=valid");
    expect(h.asked.at(-1)?.action).toBe("connect_status");
    resolve({ flow });
    await checking;
    await submitting;
    expect(h.asked.at(-1)).toEqual({ action: "connect_finish", flow_id: "aws-flow", callback: "http://127.0.0.1:9876/plugins/oauth/callback?state=right&code=valid" });

    next = async () => {
      throw coded("This connection expired. Connect again.", "plugin_request_failed");
    };
    await h.flow.check();
    expect(h.flow.state.phase).toBe("failed");
    expect(h.flow.state.flow).toBeNull();
    expect(h.stored.size).toBe(0);

    await h.flow.connect("aws");
    next = () => new Promise((r) => (resolve = r));
    const checking2 = h.flow.check();
    const submitting2 = h.flow.submitAddress("http://127.0.0.1:9876/plugins/oauth/callback?state=right&code=valid");
    await h.flow.cancel();
    const finishes = h.asked.filter((r) => r.action === "connect_finish").length;
    resolve({ flow });
    await checking2;
    await submitting2;
    expect(h.flow.state.phase).toBe("cancelled");
    expect(h.flow.state.flow).toBeNull();
    expect(h.asked.filter((r) => r.action === "connect_finish")).toHaveLength(finishes);
  });

  test("a computer that cannot be asked yet keeps the sign-in waiting", async () => {
    let fail = false;
    const h = harness(() => {
      if (fail) throw new Error("Server disconnected. Reconnect to check the result.");
      return { flow: oauth() };
    });
    await h.flow.connect("notion");
    fail = true;
    await h.flow.check();
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.flow.state.flow?.flow.id).toBe("f1");
  });

  test("Google Workspace asks for its client once, then for the address the sign-in ended on", async () => {
    const setup = oauth({ id: "gws-flow", integration: "gws", authorization_url: "https://console.cloud.google.com/auth/overview?project=mewla-ws-0a1b2c3d", code_required: true, setup_required: true, message: "Set up the consent screen." });
    let answer: ConnectionResponse = { flow: setup };
    const h = harness(() => answer);
    await h.flow.connect("gws");
    expect(h.flow.state.flow?.flow.setup_required).toBe(true);
    expect(h.asked.find((r) => r.action === "connect_start")?.input?.integration).toBe("gws");
    const client = "123456789012-abc.apps.googleusercontent.com GOCSPX-secret_value";
    answer = { flow: { ...setup, code_required: false, setup_required: false, address_required: true, authorization_url: "https://accounts.google.com/o/oauth2/auth?redirect_uri=http://localhost:42503" } };
    await h.flow.submitAddress(client);
    expect(h.asked.at(-1)).toEqual({ action: "connect_finish", flow_id: "gws-flow", callback: client });
    expect(h.flow.state.flow?.flow.address_required).toBe(true);
    expect(h.flow.state.flow?.flow.setup_required).toBe(false);
  });

  test("an install failure remains retryable in the same flow, and nothing polls while a CLI waits to be installed", async () => {
    const install = oauth({ id: "gcp-install", integration: "gcp", authorization_url: "", install_required: true });
    let answer: ConnectionResponse = { flow: install };
    const h = harness(() => answer);
    await h.flow.connect("gcp");
    expect(h.polls).toHaveLength(0);
    answer = { flow: { ...install, install_required: false, installing: true } };
    await h.flow.submitAddress("");
    expect(h.flow.state.flow?.flow.installing).toBe(true);
    answer = { flow: { ...install, install_failed: true, message: "Couldn't download gcloud. Check your connection and retry." } };
    await h.flow.check();
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.flow.state.flow?.flow.id).toBe("gcp-install");
    expect(h.flow.state.flow?.flow.install_failed).toBe(true);
    answer = { flow: { ...install, install_required: false, code_required: true, authorization_url: "https://accounts.google.com/o/oauth2/auth" } };
    await h.flow.submitAddress("");
    expect(h.flow.state.flow?.flow.code_required).toBe(true);
    expect(h.asked.at(-1)).toEqual({ action: "connect_finish", flow_id: "gcp-install", callback: "" });
  });

  test("tool switches move at once and go back when the computer refuses", async () => {
    let refuse = false;
    const account = { id: "a1", integration: "github", name: "GitHub", identity: "me", enabled: true, status: "connected" as const, history: [], tools: [{ name: "search", description: "Search", allowed: false }] };
    const h = harness(() => {
      if (refuse) throw new Error("Policy rejected.");
      return { account: { ...account, tools: [{ ...account.tools[0], allowed: true }] } };
    });
    await h.flow.load("a1");
    expect(h.flow.state.details.a1.tools?.[0].allowed).toBe(true);
    refuse = true;
    h.flow.setTools(h.flow.state.details.a1, ["search"], false);
    expect(h.flow.state.details.a1.tools?.[0].allowed).toBe(false);
    await Bun.sleep(0);
    await Bun.sleep(0);
    expect(h.flow.state.details.a1.tools?.[0].allowed).toBe(true);
    expect(h.flow.state.error).toBe("Policy rejected.");
  });

  test("after the computer is no longer current nothing it answers changes the page", async () => {
    let resolve!: (value: ConnectionResponse) => void;
    const h = harness(() => new Promise((r) => (resolve = r)));
    const listing = h.flow.list();
    h.flow.dispose();
    resolve({ catalog: [{ id: "github", name: "GitHub", available: true, setup_url: "", description: "" }], accounts: [] });
    await listing;
    expect(h.flow.state.catalog).toEqual([]);
  });
});

describe("in a browser", () => {
  function web(answer: Parameters<typeof harness>[0]) {
    const h = harness(answer);
    const here: string[] = [];
    const flow = new ConnectionsFlow({ ...h.deps, web: { origin: async () => "https://desk.example", openHere: (url) => void here.push(url) } });
    return { ...h, flow, here };
  }

  test("connect_start names the page's origin; a provider that returns to it takes this tab, any other opens a tab of its own", async () => {
    const h = web((request) => (request.action === "connect_start" ? { flow: oauth({ web_return: true }) } : { flow: oauth({ web_return: true }) }));
    await h.flow.connect("notion");
    expect(h.asked[0]).toEqual({ action: "connect_start", input: { integration: "notion", allow_writes: false, web_origin: "https://desk.example" } });
    expect(h.here).toEqual(["https://notion.example/authorize?state=abc123&client_id=x"]);
    expect(h.opened).toEqual([]);
    expect(h.stored.has(pendingConnectionKey("server"))).toBe(true);

    const coded = web(() => ({ flow: oauth({ integration: "github", user_code: "AB-CD", authorization_url: "https://github.com/login/device" }) }));
    await coded.flow.connect("github");
    await coded.flow.openBrowser();
    expect(coded.opened).toEqual(["https://github.com/login/device"]);
    expect(coded.here).toEqual([]);
  });

  test("a custom service with a token is added as before, without an origin", async () => {
    const h = web(() => ({ account: { id: "m1", integration: "mcp", name: "Mine", identity: "", enabled: true, status: "connected", history: [] } }));
    await h.flow.connect("mcp", { input: { integration: "mcp", name: "Mine", endpoint: "https://mcp.example" }, signIn: false });
    expect(h.asked[0]).toEqual({ action: "add", input: { integration: "mcp", name: "Mine", endpoint: "https://mcp.example" } });
  });

  test("a return to /plugins?connected= shows the account connected and drops the stored flow it ended", async () => {
    const h = web(() => ({ flow: oauth() }));
    h.stored.set(pendingConnectionKey("server"), JSON.stringify({ serverId: "server", flow: oauth() }));
    await h.flow.restore(null, { connected: "a1", service: "notion" });
    expect(h.flow.state.phase).toBe("connected");
    expect(h.flow.state.connectedId).toBe("a1");
    expect(h.flow.state.service).toBe("notion");
    expect(h.stored.size).toBe(0);
    expect(h.asked).toEqual([]);
  });

  test("a return with plugin_error says which step failed; a cancelled one reads as cancelled", async () => {
    const failed = web(() => ({}));
    await failed.flow.restore(null, { plugin_error: "token", service: "notion" });
    expect(failed.flow.state.phase).toBe("failed");
    expect(failed.flow.state.connectError).toBe("The service didn’t accept the sign-in. Start again.");

    const cancelled = web(() => ({}));
    await cancelled.flow.restore(null, { plugin_error: "cancelled" });
    expect(cancelled.flow.state.phase).toBe("cancelled");
    expect(cancelled.flow.state.service).toBeNull();
  });

  test("a return for another service leaves that service's waiting sign-in waiting", async () => {
    const h = web(() => ({ flow: oauth() }));
    h.stored.set(pendingConnectionKey("server"), JSON.stringify({ serverId: "server", flow: oauth() }));
    await h.flow.restore(null, { connected: "l1", service: "linear" });
    expect(h.flow.state.phase).toBe("waiting");
    expect(h.flow.state.service).toBe("notion");
    expect(h.stored.has(pendingConnectionKey("server"))).toBe(true);
  });
});
