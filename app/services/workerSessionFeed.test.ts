import { describe, expect, test } from "bun:test";
import { WorkerSessionFeed } from "./workerSessionFeed";

const list = (seq: number) => ({ type: "worker_session_list", seq });
const change = (seq: number, type = "worker_session_updated") => ({ type, seq });

describe("WorkerSessionFeed", () => {
  test("applies changes one past the list, in order", () => {
    const feed = new WorkerSessionFeed();
    expect(feed.accept(list(4))).toBe("apply");
    expect(feed.accept(change(5))).toBe("apply");
    expect(feed.accept(change(6, "worker_session_archived"))).toBe("apply");
    expect(feed.accept(change(7, "worker_session_created"))).toBe("apply");
  });

  test("drops duplicates and changes the list already covers", () => {
    const feed = new WorkerSessionFeed();
    feed.accept(list(10));
    expect(feed.accept(change(9))).toBe("drop");
    expect(feed.accept(change(10))).toBe("drop");
    expect(feed.accept(change(11))).toBe("apply");
    expect(feed.accept(change(11))).toBe("drop");
    expect(feed.accept(change(5))).toBe("drop");
  });

  test("drops changes that arrive before the first list of a connection", () => {
    const feed = new WorkerSessionFeed();
    expect(feed.accept(change(1))).toBe("drop");
    feed.accept(list(3));
    expect(feed.accept(change(4))).toBe("apply");
  });

  test("a gap asks for one resync, drops what follows, and the list ends it", () => {
    const feed = new WorkerSessionFeed();
    feed.accept(list(1));
    expect(feed.accept(change(2))).toBe("apply");
    expect(feed.accept(change(4))).toBe("resync");
    // Still waiting for the answer: no second request, no applying out of order.
    expect(feed.accept(change(5))).toBe("drop");
    expect(feed.accept(change(2))).toBe("drop");
    expect(feed.accept(list(5))).toBe("apply");
    expect(feed.accept(change(6))).toBe("apply");
    // A later gap may ask again.
    expect(feed.accept(change(8))).toBe("resync");
  });

  test("a resync that could not be sent can be asked for again", () => {
    const feed = new WorkerSessionFeed();
    feed.accept(list(1));
    expect(feed.accept(change(3))).toBe("resync");
    feed.resyncNotSent();
    expect(feed.accept(change(4))).toBe("resync");
  });

  test("a new connection starts over, even at a lower seq", () => {
    const feed = new WorkerSessionFeed();
    feed.accept(list(100));
    feed.accept(change(101));
    feed.reset();
    expect(feed.accept(change(102))).toBe("drop");
    expect(feed.accept(list(0))).toBe("apply");
    expect(feed.accept(change(1))).toBe("apply");
  });

  test("a message without a usable seq is never applied", () => {
    const feed = new WorkerSessionFeed();
    expect(feed.accept({ type: "worker_session_list" })).toBe("drop");
    expect(feed.accept({ type: "worker_session_list", seq: "1" })).toBe("drop");
    expect(feed.accept({ type: "worker_session_list", seq: -1 })).toBe("drop");
    feed.accept(list(1));
    expect(feed.accept({ type: "worker_session_updated" })).toBe("drop");
    expect(feed.accept(change(2.5))).toBe("drop");
  });
});
