import { describe, expect, test } from "bun:test";
import { decodeComputers, otherComputerState, othersNeedYou, peerOf, relayDiagnosis, whenWords } from "./computers";

const when = (at: number) => `@${new Date(at).toISOString().slice(11, 16)}`;
const B = "b6f5682c98812680dae5124894273ee93adcf22de968007d33df9f05a212dc3a";

// WIRE §5's example: B ("studio") online with a Brain Work question, from A's WebSocket.
const frame = {
  type: "computers",
  computers: [
    { daemon_id: B, last_seen: "2026-11-12T07:03:03.341752011Z", name: "studio", online: true, open_requests: 1, version: "0.3.1" },
    { daemon_id: "C".repeat(64), last_seen: "2026-11-12T05:00:00Z", name: "lab", online: false, open_requests: 4 },
    { name: "never", online: false },
  ],
  open_requests: 0,
  connection: { phase: "offline", cause: "relay unreachable", connected_at: "2026-11-12T09:00:00Z", last_failure: "relay unreachable", last_failure_at: "2026-11-12T13:40:00Z" },
};

describe("the computers message", () => {
  test("reads each paired computer; an offline one's count is unknown, not what it last had", () => {
    const view = decodeComputers(frame)!;
    expect(view.openRequests).toBe(0);
    expect(view.computers.map((c) => [c.name, c.online, c.openRequests])).toEqual([["studio", true, 1], ["lab", false, undefined], ["never", false, undefined]]);
    expect(view.computers[0]!.lastSeen).toBe(Date.parse("2026-11-12T07:03:03.341Z"));
    expect(view.connection).toMatchObject({ phase: "offline", lastFailure: "relay unreachable", lastFailureAt: Date.parse("2026-11-12T13:40:00Z") });
    expect(decodeComputers({ type: "computers" })).toBeNull();
    expect(decodeComputers({ type: "computers", computers: [] })).toEqual({ computers: [], openRequests: 0, connection: undefined });
  });

  test("a stored computer is matched by its daemon id, whatever its case", () => {
    const view = decodeComputers(frame);
    expect(peerOf(view, { daemonId: B.toUpperCase() })?.name).toBe("studio");
    expect(peerOf(view, { daemonId: "c".repeat(64) })?.name).toBe("lab");
    expect(peerOf(view, { daemonId: "f".repeat(64) })).toBeUndefined();
    expect(peerOf(null, { daemonId: B })).toBeUndefined();
  });

  test("rows: Online, Online · N need you, Offline · last seen, Pair again from the phone's own record", () => {
    const view = decodeComputers(frame)!;
    const [studio, lab, never] = view.computers;
    expect(otherComputerState({ peer: studio, when })).toEqual({ word: "Online · 1 needs you", needsYou: true, dim: false });
    expect(otherComputerState({ peer: { ...studio!, openRequests: 3 }, when }).word).toBe("Online · 3 need you");
    expect(otherComputerState({ peer: { ...studio!, openRequests: undefined }, when })).toEqual({ word: "Online", needsYou: false, dim: false });
    expect(otherComputerState({ peer: lab, when })).toEqual({ word: "Offline · last seen @05:00", needsYou: false, dim: true });
    expect(otherComputerState({ peer: never, when }).word).toBe("Offline");
    expect(otherComputerState({ peer: studio, check: { state: "unpaired" }, when })).toEqual({ word: "Pair again", needsYou: true, dim: false });
    // Not known to the current computer: what the phone found, else nothing.
    expect(otherComputerState({ check: { state: "online" }, when }).word).toBe("Online");
    expect(otherComputerState({ check: { state: "offline" }, when }).dim).toBe(true);
    expect(otherComputerState({ when }).word).toBe("");
  });

  test("the cat's dot from other computers: an online one with requests; offline is unknown", () => {
    expect(othersNeedYou(decodeComputers(frame))).toBe(true);
    expect(othersNeedYou(decodeComputers({ ...frame, computers: [frame.computers[1]] }))).toBe(false);
    expect(othersNeedYou(null)).toBe(false);
  });

  test("the relay diagnosis is the newest failure with when its stretch began; none while connected", () => {
    const view = decodeComputers(frame)!;
    expect(relayDiagnosis(view.connection, when)).toBe("Relay unreachable · @13:40");
    expect(relayDiagnosis({ phase: "offline", cause: "relay refused the key" }, when)).toBe("Relay refused the key");
    expect(relayDiagnosis({ phase: "connected", lastFailure: "relay unreachable", lastFailureAt: 1 }, when)).toBeNull();
    expect(relayDiagnosis({ phase: "connecting" }, when)).toBeNull();
    expect(relayDiagnosis(undefined, when)).toBeNull();
  });

  test("a time reads as the clock today, else as the date", () => {
    const format = { time: () => "13:40", shortDate: () => "Nov 11", sameDay: (a: number, b: number) => Math.floor(a / 864e5) === Math.floor(b / 864e5) };
    const now = Date.parse("2026-11-12T15:00:00Z");
    expect(whenWords(Date.parse("2026-11-12T13:40:00Z"), now, format)).toBe("13:40");
    expect(whenWords(Date.parse("2026-11-11T13:40:00Z"), now, format)).toBe("Nov 11");
  });
});
