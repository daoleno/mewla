import { describe, expect, test } from "bun:test";
import { PHONE_PROTOCOL_VERSION, parseDaemonFrame } from "./phoneProtocol";

describe("phone protocol", () => {
  test("a request frame is read as sent by the daemon", () => {
    const frame = parseDaemonFrame(JSON.stringify({ type: "request", id: "r1", op: "tap", args: { x: 1, y: 2 }, image: true }));
    expect(frame).toEqual({ type: "request", id: "r1", op: "tap", args: { x: 1, y: 2 }, image: true });
  });

  test("anything that is not a typed JSON object is ignored", () => {
    expect(parseDaemonFrame("not json")).toBeNull();
    expect(parseDaemonFrame("[]")).toBeNull();
    expect(parseDaemonFrame(JSON.stringify({ id: "r1" }))).toBeNull();
  });

  test("the version matches daemon/phone/protocol.go", () => {
    expect(PHONE_PROTOCOL_VERSION).toBe(1);
  });
});
