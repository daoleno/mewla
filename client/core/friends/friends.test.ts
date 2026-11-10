import { describe, expect, test } from "bun:test";
import { installMemoryPlatform } from "../testing/memoryPlatform";
import type { StoredServer } from "../storedServerContract";
import type { FriendsPost } from "./api";
import { appendPage, toggled, withCursor } from "./paging";
import { FriendsError, friendsCall, getFriendsStatus } from "./requests";
import { pollInterval, signInComputer } from "./signIn";

const server = { id: "s1", name: "Desk", url: "ws://desk.test:9877/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) } as StoredServer;
const post = (id: string): FriendsPost => ({
  id,
  text: id,
  via: "approved",
  created_at: 1,
  author: { id: "a", handle: "lena", agent_name: "", pet_id: "" },
  likes: 1,
  comments: 0,
  liked: false,
  bookmarked: false,
  mine: false,
});

describe("Friends lists", () => {
  test("the next page is asked for after the last cursor, and a post shown twice stays once", () => {
    expect(withCursor("/feed", "")).toBe("/feed");
    expect(withCursor("/feed", "17_p9")).toBe("/feed?before=17_p9");
    expect(withCursor("/feed?scope=following", "17_p 9")).toBe("/feed?scope=following&before=17_p%209");
    expect(appendPage([post("p1"), post("p2")], [post("p2"), post("p3")]).map((p) => p.id)).toEqual(["p1", "p2", "p3"]);
  });

  test("a like shows at once, and taking it back takes the count down", () => {
    const liked = toggled(post("p1"), "like");
    expect([liked.liked, liked.likes]).toEqual([true, 2]);
    expect(toggled(liked, "like").likes).toBe(1);
    expect(toggled(post("p1"), "bookmark").bookmarked).toBe(true);
  });
});

describe("the computer's /friends door", () => {
  test("a call is one signed POST whose signature covers the body; a refusal becomes a sentence", async () => {
    const seen: Array<{ url: string; method?: string; body?: string; auth: string }> = [];
    installMemoryPlatform({
      fetch: async (url, init) => {
        const headers = (init?.headers ?? {}) as Record<string, string>;
        seen.push({ url: String(url), method: init?.method, body: init?.body as string, auth: headers.Authorization });
        if (init?.method === "GET") return new Response(JSON.stringify({ signed_in: true, enabled: false, drafts: [], max_drafts: 5 }));
        return new Response(JSON.stringify({ status: 429, error: "slow_down" }));
      },
    });
    expect((await getFriendsStatus(server)).signed_in).toBe(true);
    const failure = await friendsCall(server, "PUT", "/posts/p1/like").catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(FriendsError);
    expect((failure as FriendsError).code).toBe("slow_down");
    expect((failure as Error).message).toBe("Too many tries; wait a little and try again");
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual(["GET http://desk.test:9877/friends", "POST http://desk.test:9877/friends"]);
    expect(JSON.parse(seen[1].body!)).toEqual({ op: "call", method: "PUT", path: "/posts/p1/like", body: null });
    expect(seen[1].auth).not.toBe(seen[0].auth);
  });

  test("a refusal the computer sends as an error status keeps its reason", async () => {
    installMemoryPlatform({ fetch: async () => new Response(JSON.stringify({ error: "refused", message: "It contains an email address" }), { status: 422 }) });
    const failure = (await friendsCall(server, "GET", "/feed").catch((error: unknown) => error)) as FriendsError;
    expect([failure.code, failure.message]).toEqual(["refused", "It contains an email address"]);
  });
});

describe("signing the computer in", () => {
  const start = { login_id: "L1", user_code: "ABCD", verification_uri: "https://mewla.test/device", interval_seconds: 1, expires_at_ms: 10_000 };

  test("polls no faster than every 2 s until the code is entered, showing it meanwhile", async () => {
    let now = 0;
    let polls = 0;
    const shown: Array<string | null> = [];
    const waits: number[] = [];
    const done = await signInComputer({
      start: async () => start,
      poll: async () => ++polls === 2,
      now: () => now,
      wait: async (ms) => void waits.push((now += ms)),
      show: (s) => shown.push(s?.user_code ?? null),
      current: () => true,
    });
    expect(done).toBe(true);
    expect(waits).toEqual([2000, 4000]);
    expect(shown).toEqual(["ABCD", null]);
    expect(pollInterval({ interval_seconds: 0 })).toBe(5000);
  });

  test("an expired code says so, and a cancel stops polling", async () => {
    let now = 0;
    const expired = signInComputer({ start: async () => start, poll: async () => false, now: () => now, wait: async (ms) => void (now += ms * 3), show: () => {}, current: () => true });
    await expect(expired).rejects.toThrow("The sign-in code expired; try again");
    let live = true;
    let polls = 0;
    const cancelled = await signInComputer({
      start: async () => start,
      poll: async () => (polls++, false),
      now: () => 0,
      wait: async () => void (live = false),
      show: () => {},
      current: () => live,
    });
    expect([cancelled, polls]).toEqual([false, 0]);
  });
});
