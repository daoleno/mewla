import { describe, expect, open, test } from "nsh/test";
import { answering } from "../../testing/fixtures";

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
    await answering(app, () => app.press("id:friends-auto-post"));
    expect(app.dialogs()[0].title).toBe("Post without asking?");
    expect(c.ops.some((op) => op.op === "auto_post")).toBe(false);
    app.onDialog(() => "Turn on");
    await answering(app, () => app.press("id:friends-auto-post"));
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

  test("in a browser, with Friends off, Open Labs leads to Settings, which offers no Friends switch there", async () => {
    const c = computer({ signed_in: true, enabled: false, handle: "me", drafts: [], max_drafts: 5 }, () => undefined, { name: "friends" });
    const app = await open(HARNESS, { ...c.options, platform: "web" });
    await app.waitFor("text:Friends is off");
    await app.press("id:friends-off-action");
    await app.waitFor("id:settings");
    expect(app.shows("id:friends-switch")).toBe(false);
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
    await answering(app, () => app.press("id:more-p1"), 2);
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

  test("back on the page from a post, newer ones show and are marked read too", async () => {
    const note = (seq: number, handle: string) => ({ seq, kind: "comment", actor: { ...lena, id: `u-${handle}`, handle }, post_id: "p1", post_text: "Nice", created_at: Date.now(), read: false });
    let notes = [note(1, "lena")];
    const c = computer(on(), (op) => {
      if (op.path === "/notifications" && op.method === "GET") return { status: 200, body: { notifications: notes } };
      if (op.path === "/notifications/read") return { status: 200, body: { ok: true } };
      if (op.path === "/posts/p1") return { status: 200, body: { post: post("p1"), comments: [] } };
      if (op.path === "/people/u-lena") return { status: 200, body: { ...lena, bio: "", followers: 1, following: 1, posts: 1, followed: false, blocked: false, mine: false, created_at: 0 } };
      return { status: 404 };
    }, { name: "friends-notifications" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:@lena commented on your post");
    await app.press("id:notification-1");
    await app.waitFor("id:friends-post-text");
    notes = [note(2, "june"), note(1, "lena")];
    await app.back();
    await app.waitFor("text:@june commented on your post");
    await app.waitUntil(() => c.ops.filter((op) => op.path === "/notifications/read").length >= 2);
  });
});

describe("coming back to a page", () => {
  test("the feed reads the computer again: a post deleted on its page is gone, a new one shows", async () => {
    let feed = [post("p1", { mine: true, author: { ...lena, id: "u-me", handle: "me" } }), post("p2")];
    const c = computer(on(), (op) => {
      if (op.method === "GET" && op.path === "/feed") return { status: 200, body: { posts: feed, next: "" } };
      if (op.method === "GET" && op.path === "/posts/p1") return { status: 200, body: { post: feed[0], comments: [] } };
      if (op.method === "DELETE" && op.path === "/posts/p1") {
        feed = [post("p3"), post("p2")];
        return { status: 200, body: { ok: true } };
      }
      return { status: 404 };
    }, { name: "friends" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    await app.press("id:open-post-p1");
    await app.waitFor("id:friends-post-text");
    app.onDialog((dialog) => (dialog.sheet ? "Delete post" : "Delete"));
    await answering(app, () => app.press("id:friends-post-more"), 2);
    await app.waitFor("id:friends-feed");
    await app.waitFor("text:Post p3");
    expect(app.texts()).not.toContain("Post p1");
    expect(app.texts()).toContain("Post p2");
  });

  test("a profile reads the person again: a follow made on another page shows", async () => {
    const person = (followed: boolean) => ({ ...lena, bio: "", followers: followed ? 5 : 4, following: 2, posts: 1, followed, blocked: false, mine: false, created_at: 0 });
    let followed = false;
    const c = computer(on(), (op) => {
      if (op.path === "/people/u-lena" && op.method === "GET") return { status: 200, body: person(followed) };
      if (op.path === "/people/u-lena/posts") return { status: 200, body: { posts: [post("p1")], next: "" } };
      if (op.path === "/posts/p1" && op.method === "GET") return { status: 200, body: { post: post("p1"), comments: [] } };
      return { status: 404 };
    }, { name: "friends-person", id: "u-lena" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:1 posts  2 following  4 followers");
    await app.waitFor("text:Post p1");
    await app.press("id:open-post-p1");
    await app.waitFor("id:friends-post-text");
    followed = true;
    await app.back();
    await app.waitFor("text:1 posts  2 following  5 followers");
    expect(app.texts()).toContain("Following");
  });
});

describe("a switch of computer on an open page", () => {
  const other = "http://other.test:9877/friends";

  test("the feed drops the last computer's posts, and a like goes to the computer whose posts show", async () => {
    const c = computer(on(), (op) => (op.method === "GET" && op.path === "/feed" ? { status: 200, body: { posts: [post("p1")], next: "" } } : { status: 404 }), { name: "friends" });
    const otherOps: Op[] = [];
    const network = c.options.network as Record<string, unknown>;
    network[`GET ${other}`] = () => ({ body: on() });
    network[`POST ${other}`] = (request: { body?: string | null }) => {
      const op = JSON.parse(request.body ?? "{}") as Op;
      otherOps.push(op);
      if (op.method === "GET" && op.path === "/feed") return { body: { status: 200, body: { posts: [post("q1", { text: "Laptop post" })], next: "" } } };
      if (op.method === "PUT" && op.path === "/posts/q1/like") return { body: { status: 200, body: post("q1", { text: "Laptop post", liked: true, likes: 2 }) } };
      return { body: { status: 404 } };
    };
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop", url: "ws://other.test:9877/ws" });
    await app.waitFor("text:Post p1", { state: "gone" });
    await app.waitFor("text:Laptop post");
    await app.press("id:like-q1");
    await app.waitUntil(() => otherOps.some((op) => op.method === "PUT" && op.path === "/posts/q1/like"));
    expect(c.ops.some((op) => op.method === "PUT")).toBe(false);
  });

  test("a post page reads the post again from the next computer", async () => {
    const c = computer(on(), (op) => (op.path === "/posts/p1" ? { status: 200, body: { post: post("p1", { mine: true }), comments: [] } } : { status: 404 }), { name: "friends-post", id: "p1" });
    const network = c.options.network as Record<string, unknown>;
    const otherOps: Op[] = [];
    network[`GET ${other}`] = () => ({ body: on() });
    network[`POST ${other}`] = (request: { body?: string | null }) => {
      const op = JSON.parse(request.body ?? "{}") as Op;
      otherOps.push(op);
      if (op.path === "/posts/p1") return { body: { status: 200, body: { post: post("p1", { mine: true, text: "As the laptop sees it", comments: 1 }), comments: [{ id: "c9", text: "From the laptop", created_at: Date.now(), author: lena, mine: false }] } } };
      return { body: { status: 404 } };
    };
    const app = await open(HARNESS, c.options);
    await app.waitFor("text:Post p1");
    await app.host.send("app.daemonSwitch", { id: "s2", name: "Laptop", url: "ws://other.test:9877/ws" });
    await app.waitFor("text:As the laptop sees it");
    expect(app.texts()).toContain("From the laptop");
    expect(app.texts()).not.toContain("Post p1");
  });
});

describe("a post's video", () => {
  const video = (seconds: number) => [{ kind: "video", url: `https://cdn.test/v${seconds}.mp4`, width: 1280, height: 720, bytes: 10, duration_ms: seconds * 1000, poster: { url: `https://cdn.test/v${seconds}.jpg`, width: 1280, height: 720 } }];

  test("plays muted, one at a time; sound and full screen are a tap away", async () => {
    const c = computer(on(), (op) => (op.method === "GET" && op.path === "/feed" ? { status: 200, body: { posts: [post("p1", { media: video(12) }), post("p2", { media: video(7) })], next: "" } } : { status: 404 }), { name: "friends" });
    const app = await open(HARNESS, { ...c.options, network: { ...c.options.network, "GET https://cdn.test/*": { status: 404 } } });
    await app.waitFor("label:Play video, 0:12");
    expect(app.count("id:friends-video-player")).toBe(0);
    await app.press("label:Play video, 0:12");
    await app.waitFor("id:friends-video-player");
    expect(app.shows("label:Turn sound on")).toBe(true);
    await app.press("id:friends-video-sound");
    await app.waitFor("label:Turn sound off");
    await app.press("label:Play video, 0:07");
    await app.waitFor("label:Play video, 0:12");
    expect(app.count("id:friends-video-player")).toBe(1);
    expect(app.shows("label:Turn sound on")).toBe(true);
    await app.press("id:friends-video-full-screen");
    await app.waitFor("id:friends-video-close");
    await app.press("id:friends-video-close");
    await app.waitFor("id:friends-video-close", { state: "gone" });
    expect(app.count("id:friends-video-player")).toBe(1);
  });

  test("stops when another page opens over it", async () => {
    const c = computer(on(), (op) => {
      if (op.method === "GET" && op.path === "/feed") return { status: 200, body: { posts: [post("p1", { media: video(12) })], next: "" } };
      if (op.path === "/posts/p1") return { status: 200, body: { post: post("p1", { media: video(12) }), comments: [] } };
      return { status: 404 };
    }, { name: "friends" });
    const app = await open(HARNESS, { ...c.options, network: { ...c.options.network, "GET https://cdn.test/*": { status: 404 } } });
    await app.waitFor("label:Play video, 0:12");
    await app.press("label:Play video, 0:12");
    await app.waitFor("id:friends-video-player");
    await app.press("id:open-post-p1");
    await app.waitFor("id:friends-post-text");
    await app.back();
    await app.waitFor("label:Play video, 0:12");
    expect(app.count("id:friends-video-player")).toBe(0);
  });
});

describe("the post page", () => {
  test("Follow sits beside the author, and pressing it does not open the profile", async () => {
    const c = computer(on(), (op) => {
      if (op.path === "/posts/p1" && op.method === "GET") return { status: 200, body: { post: post("p1"), comments: [] } };
      if (op.path === "/people/u-lena" && op.method === "GET") return { status: 200, body: { ...lena, bio: "", followers: 1, following: 1, posts: 1, followed: false, blocked: false, mine: false, created_at: 0 } };
      if (op.path === "/people/u-lena/follow" && op.method === "PUT") return { status: 200, body: { ...lena, bio: "", followers: 2, following: 1, posts: 1, followed: true, blocked: false, mine: false, created_at: 0 } };
      return { status: 404 };
    }, { name: "friends-post", id: "p1" });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:friends-follow");
    await app.press("id:friends-follow");
    await app.waitFor("text:Following");
    expect(app.shows("id:friends-person")).toBe(false);
    await app.press("id:friends-post-comment");
    await app.type("id:friends-comment", "Hi");
    expect(app.value("id:friends-comment")).toBe("Hi");
    await app.press("id:friends-post-author");
    await app.waitFor("id:friends-person");
  });
});

describe("a draft being edited", () => {
  test("keeps the owner's text when the status is read again or another draft goes", async () => {
    const draft = (id: string, text: string) => ({ id, text, created_at: new Date().toISOString() });
    let status = on({ drafts: [draft("d1", "First"), draft("d2", "Second")] });
    const c = computer(status, (op) => {
      if (op.op === "check") return { ok: true };
      if (op.op === "discard_draft") {
        status = on({ drafts: [draft("d1", "First")] });
        return { ok: true };
      }
      return undefined;
    }, { name: "friends-drafts" });
    c.options.network[STATUS] = () => ({ body: status });
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:draft-d2");
    await app.clear("id:draft-text-d1");
    await app.type("id:draft-text-d1", "My words");
    const reads = app.requests().filter((r) => r.method === "GET" && r.url === "http://desk.test:9877/friends").length;
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    await app.waitUntil(() => app.requests().filter((r) => r.method === "GET" && r.url === "http://desk.test:9877/friends").length > reads);
    expect(app.value("id:draft-text-d1")).toBe("My words");
    app.onDialog(() => "Discard");
    await answering(app, () => app.press("id:draft-discard-d2"));
    await app.waitFor("id:draft-d2", { state: "gone" });
    expect(app.value("id:draft-text-d1")).toBe("My words");
  });
});

describe("Friends settings", () => {
  test("Keep asking me leaves the switch off; deleting the account shows it is busy", async () => {
    const c = computer(on(), () => undefined, { name: "settings-friends" });
    const network = c.options.network as Record<string, unknown>;
    const door = network[DOOR] as (request: { body?: string | null }) => unknown;
    network[DOOR] = (request: { body?: string | null }) => {
      const op = JSON.parse(request.body ?? "{}") as Op;
      if (op.op === "delete_account") {
        c.ops.push(op);
        return { body: { signed_in: true, enabled: false, drafts: [], max_drafts: 5 }, delay: 400 };
      }
      return door(request);
    };
    const app = await open(HARNESS, c.options);
    await app.waitFor("id:friends-auto-post");
    app.onDialog(() => "Keep asking me");
    await answering(app, () => app.press("id:friends-auto-post"));
    expect(app.shows("text:Up to 3 a day")).toBe(false);
    expect(app.shows("id:friends-auto-media")).toBe(false);
    app.onDialog(() => "Delete account");
    await answering(app, () => app.press("id:friends-delete"));
    await app.waitFor("id:friends-deleting");
    await app.waitFor("id:friends-delete", { state: "gone" });
  });
});
