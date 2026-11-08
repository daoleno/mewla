import { describe, expect, test } from "bun:test";
import { createRfbFeed } from "./runtime/rfbFeed";

// The daemon's RFB ProtocolVersion arrives right behind the socket opening, often in the same batch as
// the host's "open": the viewer's client does not exist yet. A dropped hello leaves both ends waiting.
describe("rfb feed", () => {
  const hello = btoa("RFB 003.008\n");

  test("the hello that arrives in the batch with open reaches the client once it exists", () => {
    const feed = createRfbFeed();
    feed.opening();
    expect(feed.accept([hello])).toEqual([]);
    expect(feed.opened()).toEqual([hello]);
    expect(feed.state).toBe("open");
  });

  test("bytes keep their order across the wait and after it", () => {
    const feed = createRfbFeed();
    feed.opening();
    feed.accept(["a"]);
    feed.accept(["b", "c"]);
    expect(feed.opened()).toEqual(["a", "b", "c"]);
    expect(feed.accept(["d"])).toEqual(["d"]);
  });

  test("bytes of a session that ended are nobody's", () => {
    const feed = createRfbFeed();
    expect(feed.accept(["stray"])).toEqual([]);
    feed.opening();
    feed.accept(["old"]);
    feed.closed();
    expect(feed.accept(["late"])).toEqual([]);
    feed.opening();
    expect(feed.opened()).toEqual([]);
  });

  test("a second open starts with nothing from the first", () => {
    const feed = createRfbFeed();
    feed.opening();
    feed.accept(["first"]);
    feed.opening();
    feed.accept(["second"]);
    expect(feed.opened()).toEqual(["second"]);
  });
});
