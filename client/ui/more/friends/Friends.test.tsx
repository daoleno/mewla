import { describe, expect, open, test } from "nsh/test";

const HARNESS = "./testing/Harness.tsx";
const DOOR = "POST http://desk.test:9877/friends";
const STATUS = "GET http://desk.test:9877/friends";

function secureStore() {
  const secure = new Map<string, string>();
  return {
    "secure.get": ({ key }: { key: string }) => secure.get(key) ?? null,
    "secure.set": ({ key, value }: { key: string; value: string }) => void secure.set(key, value),
    "secure.remove": ({ key }: { key: string }) => void secure.delete(key),
  };
}

const lena = { id: "u-lena", handle: "lena", agent_name: "Miso", pet_id: "p02" };
const me = { id: "u-me", handle: "me", handle_changed: false, agent_name: "Dango", pet_id: "p05", bio: "", auto_post: false, daily_cap: 3, auto_today: 0, unread: 2 };
const post = (id: string, overrides: Record<string, unknown> = {}) => ({
  id,
  text: `Post ${id}`,
  via: "approved",
  created_at: Date.now() - 3_600_000,
  author: lena,
  likes: 1,
  comments: 0,
  liked: false,
  bookmarked: false,
  mine: false,
  ...overrides,
});

type Op = Record<string, unknown> & { op: string };

/** The computer behind /friends: `status` for GET, `answer(op)` for every POST (a `call`'s answer is `{status, body}`). */
function computer(status: Record<string, unknown>, answer: (op: Op) => unknown, page: unknown) {
  const ops: Op[] = [];
  return {
    ops,
    status,
    options: {
      host: { params: { page }, handlers: { ...secureStore(), "app.daemon": () => ({ error: "not used" }) } },
      network: {
        [STATUS]: () => ({ body: status }),
        [DOOR]: (request: { body?: string | null }) => {
          const op = JSON.parse(request.body ?? "{}") as Op;
          ops.push(op);
          const reply = answer(op);
          return reply === undefined ? { status: 500, body: { error: "local_error" } } : { body: reply };
        },
        "GET http://desk.test:9877/auth-check": { status: 503 },
      },
    },
  };
}

const on = (overrides: Record<string, unknown> = {}) => ({ signed_in: true, account: "me@example.com", enabled: true, handle: "me", me, drafts: [], max_drafts: 5, ...overrides });

describe("Friends in Labs", () => {
  test("turning it on signs the computer in with a code, then names the agent after the pet", async () => {
    let signedIn = false;
    let polls = 0;
    const c = computer({ signed_in: false, enabled: false, drafts: [], max_drafts: 5 }, (op) => {
      if (op.op === "login_start") return { login_id: "L1", user_code: "WXYZ", verification_uri: "https://mewla.test/device", verification_uri_complete: "https://mewla.test/device?code=WXYZ", interval_seconds: 1, expires_at_ms: Date.now() + 600_000 };
      if (op.op === "login_poll") return { status: ++polls >= 1 ? ((signedIn = true), "done") : "pending" };
      if (op.op === "enable") return on();
      return undefined;
    }, { name: "settings-friends" });
    c.options.network[STATUS] = () => ({ body: signedIn ? on() : c.status });
    const app = await open(HARNESS, { ...c.options, storage: { "mewla:pet_preference": "p09" } });
    await app.press("id:friends-switch");
    await app.waitFor("id:friends-code");
    expect(app.texts()).toContain("WXYZ");
    await app.press("id:friends-open-sign-in");
    expect(app.openedUrls()).toContain("https://mewla.test/device?code=WXYZ");
    await app.waitFor("id:friends-open", { timeout: 8000 });
    const enable = c.ops.find((op) => op.op === "enable")!;
    expect([enable.agent_name, enable.pet_id]).toEqual(["Tama", "p09"]);
    expect(app.texts()).toContain("@me");
    expect(app.texts()).toContain("Signed in as me@example.com");
  });

  test("Post without asking asks first, and turns on only when the person agrees", async () => {
    const c = computer(on(), (op) => (op.op === "auto_post" ? on({ me: { ...me, auto_post: op.enabled === true } }) : undefined), { name: "settings-friends" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:friends-auto-post");
    app.onDialog(() => "Keep asking me");
    await app.press("id:friends-auto-post");
    expect(app.dialogs()[0].title).toBe("Post without asking?");
    expect(c.ops.some((op) => op.op === "auto_post")).toBe(false);
    app.onDialog(() => "Turn on");
    await app.press("id:friends-auto-post");
    await app.waitFor("text:Up to 3 a day");
    expect(c.ops.find((op) => op.op === "auto_post")).toMatchObject({ enabled: true });
    await app.waitFor("id:friends-auto-media");
  });
});

describe("the Friends feed", () => {
  test("shows the posts, the waiting drafts and the bell; a like shows at once and is sent", async () => {
    const status = on({ drafts: [{ id: "d1", text: "Hi", created_at: new Date().toISOString() }] });
    const c = computer(status, (op) => {
      if (op.op !== "call") return undefined;
      if (op.method === "GET" && op.path === "/feed") return { status: 200, body: { posts: [post("p1"), post("p2")], next: "" } };
      if (op.method === "PUT" && op.path === "/posts/p1/like") return { status: 200, body: post("p1", { liked: true, likes: 2 }) };
      return { status: 404, error: "not_found" };
    }, { name: "friends" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    expect(app.texts()).toContain("Draft waiting");
    expect(app.shows("label:Notifications, 2 new")).toBe(true);
    await app.press("id:like-p1");
    await app.waitFor("label:Unlike, 2 likes");
    await app.waitUntil(() => c.ops.some((op) => op.method === "PUT"));
    expect(c.ops.find((op) => op.method === "PUT")).toMatchObject({ path: "/posts/p1/like", body: null });
  });

  test("a failed like goes back and says why", async () => {
    const c = computer(on(), (op) => {
      if (op.method === "GET") return { status: 200, body: { posts: [post("p1")], next: "" } };
      return { status: 429, error: "slow_down" };
    }, { name: "friends" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    await app.press("id:like-p1");
    await app.waitFor("text:Too many tries; wait a little and try again");
    expect(app.shows("label:Like, 1 likes")).toBe(true);
  });

  test("with Friends off it says so and leads to Labs", async () => {
    const c = computer({ signed_in: true, enabled: false, handle: "me", drafts: [], max_drafts: 5 }, () => undefined, { name: "friends" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Friends is off");
    await app.press("id:friends-off-action");
    await app.waitFor("id:friends-switch");
  });

  test("blocking someone from a post's menu asks, then their posts leave the list", async () => {
    const c = computer(on(), (op) => {
      if (op.method === "GET") return { status: 200, body: { posts: [post("p1"), post("p2", { author: { ...lena, id: "u-kim", handle: "kim" } })], next: "" } };
      if (op.method === "PUT" && op.path === "/people/u-lena/block") return { status: 200, body: { ...lena, blocked: true } };
      return { status: 404 };
    }, { name: "friends" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    app.onDialog((dialog) => (dialog.sheet ? "Block @lena" : "Block"));
    await app.press("id:more-p1");
    await app.waitFor("text:Post p1", { state: "gone" });
    expect(app.texts()).toContain("Post p2");
    const confirm = app.dialogs().find((d) => !d.sheet)!;
    expect([confirm.title, confirm.message]).toEqual(["Block @lena?", "Neither of you will see the other's posts, comments or profile."]);
  });
});

describe("a draft", () => {
  test("an edit is checked on the computer, and Post sends what was reviewed, then opens the post", async () => {
    let status = on({ drafts: [{ id: "d1", text: "Fixed the shutdown hook today.", source: "Work", created_at: new Date().toISOString(), media: [{ file: "a.jpg", kind: "image", mime: "image/jpeg", width: 10, height: 10, bytes: 10 }] }] });
    const c = computer(status, (op) => {
      if (op.op === "draft_media") return { mime: "image/png", width: 1, height: 1, data: "iVBORw0KGgo=" };
      if (op.op === "check") return String(op.text).includes("@") ? undefined : { ok: true };
      if (op.op === "post_draft") {
        status = on();
        return { post: { id: "p9" } };
      }
      if (op.op === "call" && op.path === "/posts/p9") return { status: 200, body: { post: post("p9", { text: "Fixed it.", mine: true, author: { ...lena, id: "u-me", handle: "me" } }), comments: [] } };
      return undefined;
    }, { name: "friends-drafts" });
    c.options.network[DOOR] = ((inner) => (request: { body?: string | null }) => {
      const op = JSON.parse(request.body ?? "{}") as Op;
      if (op.op === "check" && String(op.text).includes("@")) {
        c.ops.push(op);
        return { status: 422, body: { error: "refused", message: "It contains an email address" } };
      }
      return inner(request);
    })(c.options.network[DOOR]);
    c.options.network[STATUS] = () => ({ body: status });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:draft-d1");
    expect(app.texts()).toContain("Needs you");
    await app.clear("id:draft-text-d1");
    await app.type("id:draft-text-d1", "mail me@example.com");
    await app.waitFor("text:It contains an email address", { timeout: 4000 });
    expect(app.enabled("id:draft-post-d1")).toBe(false);
    await app.clear("id:draft-text-d1");
    await app.type("id:draft-text-d1", "Fixed it.");
    await app.waitFor("text:Checked: no private names or keys", { timeout: 4000 });
    await app.waitUntil(() => app.enabled("id:draft-post-d1"));
    await app.press("id:draft-post-d1");
    await app.waitFor("id:friends-post-text");
    expect(c.ops.find((op) => op.op === "post_draft")).toEqual({ op: "post_draft", draft_id: "d1", text: "Fixed it.", reviewed: ["a.jpg"] });
  });
});

describe("a post", () => {
  test("a comment is sent and added, and the count follows", async () => {
    const c = computer(on(), (op) => {
      if (op.path === "/posts/p1" && op.method === "GET") return { status: 200, body: { post: post("p1", { comments: 1 }), comments: [{ id: "c1", text: "Nice", created_at: Date.now(), author: lena, mine: false }] } };
      if (op.path === "/people/u-lena") return { status: 200, body: { ...lena, bio: "", followers: 1, following: 1, posts: 1, followed: false, blocked: false, mine: false, created_at: 0 } };
      if (op.path === "/posts/p1/comments") return { status: 200, body: { id: "c2", text: (op.body as { text: string }).text, created_at: Date.now(), author: me, mine: true } };
      return { status: 404 };
    }, { name: "friends-post", id: "p1" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Nice");
    await app.waitFor("id:friends-follow");
    expect(app.enabled("id:friends-send")).toBe(false);
    await app.type("id:friends-comment", "  Thanks for this  ");
    await app.press("id:friends-send");
    await app.waitFor("text:Thanks for this");
    await app.waitFor("text:1 like   2 comments");
    expect(c.ops.find((op) => op.path === "/posts/p1/comments")!.body).toEqual({ text: "Thanks for this" });
  });
});

describe("a switch of computer", () => {
  test("drops what the last computer said and reads the next one", async () => {
    const c = computer(on(), () => ({ status: 200, body: { posts: [], next: "" } }), { name: "settings-friends" });
    const other = { signed_in: true, enabled: false, drafts: [], max_drafts: 5 };
    (c.options.network as Record<string, unknown>)["GET http://other.test:9877/friends"] = () => ({ body: other });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:friends-open");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop", url: "ws://other.test:9877/ws" });
    await app.waitFor("id:friends-open", { state: "gone" });
    expect(app.texts()).not.toContain("@me");
    expect(app.requests().some((r) => r.method === "GET" && r.url === "http://other.test:9877/friends")).toBe(true);
  });
});

describe("a person", () => {
  const person = (overrides: Record<string, unknown> = {}) => ({ ...lena, bio: "Builds tools", followers: 4, following: 2, posts: 1, followed: false, blocked: false, mine: false, created_at: 0, ...overrides });

  test("Follow follows, and the header says so; their posts are below", async () => {
    const c = computer(on(), (op) => {
      if (op.path === "/people/u-lena" && op.method === "GET") return { status: 200, body: person() };
      if (op.path === "/people/u-lena/follow" && op.method === "PUT") return { status: 200, body: person({ followed: true, followers: 5 }) };
      if (op.path === "/people/u-lena/posts") return { status: 200, body: { posts: [post("p1")], next: "" } };
      return { status: 404 };
    }, { name: "friends-person", id: "u-lena" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    expect(app.texts()).toContain("1 posts  2 following  4 followers");
    await app.press("id:friends-follow");
    await app.waitFor("text:Following");
    await app.waitFor("text:1 posts  2 following  5 followers");
  });

  test("your own profile leads to your bookmarks and the people you blocked", async () => {
    const c = computer(on(), (op) => {
      if (op.path === "/people/u-me") return { status: 200, body: person({ id: "u-me", handle: "me", mine: true }) };
      if (op.path === "/people/u-me/posts") return { status: 200, body: { posts: [], next: "" } };
      if (op.path === "/blocks") return { status: 200, body: { people: [{ id: "u-kim", handle: "kim", agent_name: "", pet_id: "" }] } };
      if (op.path === "/people/u-kim/block" && op.method === "DELETE") return { status: 200, body: {} };
      return { status: 404 };
    }, { name: "friends-person", id: "u-me" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:What Brain posts for you shows up here.");
    await app.press("id:friends-open-blocked");
    await app.waitFor("text:@kim");
    await app.press("id:unblock-u-kim");
    await app.waitUntil(() => c.ops.some((op) => op.method === "DELETE" && op.path === "/people/u-kim/block"));
  });
});

describe("notifications", () => {
  test("likes on one post fold into one row, and opening the page marks them read", async () => {
    const note = (seq: number, kind: string, handle: string) => ({ seq, kind, actor: { ...lena, id: `u-${handle}`, handle }, post_id: kind === "follow" ? "" : "p1", post_text: "Was it the shutdown hook?", created_at: Date.now(), read: false });
    const c = computer(on({ drafts: [{ id: "d1", text: "x", created_at: new Date().toISOString() }] }), (op) => {
      if (op.path === "/notifications" && op.method === "GET") return { status: 200, body: { notifications: [note(3, "like", "lena"), note(2, "like", "june"), note(1, "follow", "kim")] } };
      if (op.path === "/notifications/read") return { status: 200, body: { ok: true } };
      return { status: 404 };
    }, { name: "friends-notifications" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:@lena and 1 other liked your post");
    expect(app.texts()).toContain("@kim followed you");
    expect(app.texts()).toContain("Dango has a draft waiting for you");
    await app.waitUntil(() => c.ops.some((op) => op.path === "/notifications/read" && op.method === "POST"));
  });
});
