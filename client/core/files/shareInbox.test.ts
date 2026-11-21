import { describe, expect, test } from "bun:test";
import { CONNECT_GRACE_MS, ShareInbox, type ShareConditions, type ShareTarget, type SharedBatch } from "./shareInbox";
import { MAX_COMPOSER_ATTACHMENTS, type UploadDocumentAsset } from "./uploads";

const asset = (name: string): UploadDocumentAsset => ({ uri: `file:///cache/share-in/b/${name}`, name, mimeType: "text/plain", size: 3 });
const batch = (id: string, over: Partial<SharedBatch> = {}): SharedBatch => ({ id, text: "", problem: null, assets: [asset(`${id}.txt`)], ...over });

function setup() {
  const state = { paired: true, draft: true, pairAgain: false, connected: true, now: 0, held: 0 };
  const log: string[] = [];
  const acked: string[][] = [];
  const timers: Array<{ at: number; run: () => void; live: boolean }> = [];
  const target: ShareTarget = {
    room: () => MAX_COMPOSER_ATTACHMENTS - state.held,
    addFiles: (assets) => { state.held += assets.length; log.push(`files:${assets.map((a) => a.name).join(",")}`); },
    addText: (text) => log.push(`text:${text}`),
    notice: (text) => log.push(`notice:${text}`),
  };
  const conditions: ShareConditions = { paired: () => state.paired, draft: () => state.draft, pairAgain: () => state.pairAgain, connected: () => state.connected, now: () => state.now };
  const landed = { count: 0 };
  const inbox = new ShareInbox(
    { take: async () => [], ack: async (ids) => void acked.push(ids) },
    conditions,
    () => (landed.count += 1),
    {
      set: (run, ms) => { const timer = { at: state.now + ms, run, live: true }; timers.push(timer); return timer as never; },
      clear: (timer) => { (timer as never as { live: boolean }).live = false; },
    },
  );
  const advance = (ms: number) => {
    state.now += ms;
    timers.filter((t) => t.live && t.at <= state.now).forEach((t) => { t.live = false; t.run(); });
  };
  return { state, log, acked, inbox, target, landed, advance };
}

describe("a share arriving", () => {
  test("goes into the composer's chips and is acknowledged", () => {
    const t = setup();
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a")]);
    expect(t.log).toEqual(["files:a.txt"]);
    expect(t.acked).toEqual([["a"]]);
    expect(t.landed.count).toBe(1);
  });

  test("is not taken twice when the host offers it again before it is acknowledged", () => {
    const t = setup();
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a")]);
    t.inbox.receive([batch("a")]);
    expect(t.log).toEqual(["files:a.txt"]);
    expect(t.landed.count).toBe(1);
  });

  test("a second share is added to the first, never replacing it", () => {
    const t = setup();
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a")]);
    t.inbox.receive([batch("b")]);
    expect(t.log).toEqual(["files:a.txt", "files:b.txt"]);
    expect(t.state.held).toBe(2);
  });

  test("shared text joins the draft with its files", () => {
    const t = setup();
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a", { text: "Title\nhttps://x.y" })]);
    expect(t.log).toEqual(["text:Title\nhttps://x.y", "files:a.txt"]);
  });

  test("a share with nothing usable says so once", () => {
    const t = setup();
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a", { assets: [], problem: "Nothing usable was shared with Mewla." })]);
    t.inbox.deliver();
    expect(t.log).toEqual(["notice:Nothing usable was shared with Mewla."]);
    expect(t.acked).toEqual([["a"]]);
  });
});

describe("a composer that asks again while it takes a share", () => {
  test("takes it once: adding to the chips is what the inbox waits on, and it is told so from inside", () => {
    const t = setup();
    let inside = 0;
    const reentrant: ShareTarget = {
      ...t.target,
      addFiles: (assets) => {
        t.target.addFiles(assets);
        if (inside++ < 3) t.inbox.deliver();
      },
    };
    t.inbox.setTarget(reentrant);
    t.inbox.receive([batch("a", { text: "t" })]);
    expect(t.log).toEqual(["text:t", "files:a.txt"]);
    expect(t.state.held).toBe(1);
    expect(t.acked).toEqual([["a"]]);
  });
});

describe("a share that cannot go in yet", () => {
  test("waits for the composer, text and files together", () => {
    const t = setup();
    t.inbox.receive([batch("a", { text: "hello" })]);
    expect(t.log).toEqual([]);
    expect(t.acked).toEqual([]);
    expect(t.inbox.pending()).toBe(1);
    t.inbox.setTarget(t.target);
    expect(t.log).toEqual(["text:hello", "files:a.txt"]);
  });

  test("waits while no computer is paired, then lands once one is", () => {
    const t = setup();
    t.state.paired = false;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a", { text: "link" })]);
    expect(t.log).toEqual([]);
    expect(t.landed.count).toBe(1);
    t.state.paired = true;
    t.inbox.deliver();
    expect(t.log).toEqual(["text:link", "files:a.txt"]);
  });

  test("the files go in (to fail with Retry) while Brain's draft is not there yet; the text waits for it", () => {
    const t = setup();
    t.state.connected = false;
    t.state.draft = false;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a", { text: "note" })]);
    t.advance(CONNECT_GRACE_MS);
    expect(t.log).toEqual(["files:a.txt"]);
    expect(t.acked).toEqual([]);
    t.state.draft = true;
    t.inbox.deliver();
    expect(t.log).toEqual(["files:a.txt", "text:note"]);
    expect(t.acked).toEqual([["a"]]);
  });

  test("waits while the computer must be paired again", () => {
    const t = setup();
    t.state.pairAgain = true;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a")]);
    expect(t.log).toEqual([]);
    t.state.pairAgain = false;
    t.inbox.deliver();
    expect(t.log).toEqual(["files:a.txt"]);
  });

  test("a computer still connecting is given a moment, then the files go in for the failed chip", () => {
    const t = setup();
    t.state.connected = false;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a", { text: "now" })]);
    // The text needs no computer.
    expect(t.log).toEqual(["text:now"]);
    t.advance(CONNECT_GRACE_MS - 1);
    expect(t.log).toEqual(["text:now"]);
    t.advance(1);
    expect(t.log).toEqual(["text:now", "files:a.txt"]);
    expect(t.acked).toEqual([["a"]]);
  });

  test("a computer that connects inside the moment takes the files at once", () => {
    const t = setup();
    t.state.connected = false;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a")]);
    t.advance(2_000);
    expect(t.log).toEqual([]);
    t.state.connected = true;
    t.inbox.deliver();
    expect(t.log).toEqual(["files:a.txt"]);
  });
});

describe("more files than the composer holds", () => {
  const many = (n: number) => Array.from({ length: n }, (_, i) => asset(`p${i}.jpg`));

  test("what fits goes in, the rest waits, and the person is told once", () => {
    const t = setup();
    t.state.held = 3;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a", { assets: many(8) })]);
    expect(t.state.held).toBe(MAX_COMPOSER_ATTACHMENTS);
    expect(t.log.filter((line) => line.startsWith("notice:"))).toEqual([`notice:Choose up to ${MAX_COMPOSER_ATTACHMENTS} files. Remove an attachment to make room.`]);
    expect(t.acked).toEqual([]);
    t.inbox.deliver();
    expect(t.log.filter((line) => line.startsWith("notice:")).length).toBe(1);
    // Room again (a message went out): the rest follows, and the share is whole.
    t.state.held = 0;
    t.inbox.deliver();
    expect(t.state.held).toBe(3);
    expect(t.acked).toEqual([["a"]]);
  });

  test("a later share does not pass one still waiting for room", () => {
    const t = setup();
    t.state.held = MAX_COMPOSER_ATTACHMENTS;
    t.inbox.setTarget(t.target);
    t.inbox.receive([batch("a"), batch("b")]);
    t.state.held = MAX_COMPOSER_ATTACHMENTS - 1;
    t.inbox.deliver();
    expect(t.log.filter((line) => line.startsWith("files:"))).toEqual(["files:a.txt"]);
    t.state.held = 0;
    t.inbox.deliver();
    expect(t.log.filter((line) => line.startsWith("files:"))).toEqual(["files:a.txt", "files:b.txt"]);
  });
});

describe("pulling from the host", () => {
  test("takes what waits and lands the app on Brain", async () => {
    const t = setup();
    const inbox = new ShareInbox({ take: async () => [batch("x")], ack: async () => {} }, { paired: () => true, draft: () => true, pairAgain: () => false, connected: () => true, now: () => 0 }, () => (t.landed.count += 1));
    inbox.setTarget(t.target);
    await inbox.pull();
    expect(t.log).toEqual(["files:x.txt"]);
    expect(t.landed.count).toBe(1);
  });

  test("a host that cannot answer (an older app, a build without it) is not an error", async () => {
    const inbox = new ShareInbox({ take: async () => { throw new Error("no handler"); }, ack: async () => {} }, { paired: () => true, draft: () => true, pairAgain: () => false, connected: () => true, now: () => 0 });
    await inbox.pull();
    expect(inbox.pending()).toBe(0);
  });
});
