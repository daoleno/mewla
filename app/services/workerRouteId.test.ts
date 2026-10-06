import { describe, expect, test } from "bun:test";
import { decodeWorkerRouteId, encodeWorkerRouteId } from "./workerRouteId";

describe("worker route IDs", () => {
  test("round-trip tmux pane IDs without exposing a raw percent escape", () => {
    const routeId = encodeWorkerRouteId("%152");
    expect(routeId).toBe("%25152");
    expect(decodeWorkerRouteId(routeId)).toBe("%152");
  });

  test("round-trip legacy punctuation and unicode display identities", () => {
    for (const workerId of ["zen-worker-brain:@1", "会话:@7"]) {
      expect(decodeWorkerRouteId(encodeWorkerRouteId(workerId))).toBe(workerId);
    }
  });
});
