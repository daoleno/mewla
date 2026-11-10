import { describe, expect, test } from "bun:test";
import { decodeWorkerRouteId, encodeWorkerRouteId, resolveTerminalLink, terminalRouteParams } from "./workerRouteId";

describe("worker route IDs", () => {
  test("round-trip tmux pane IDs without exposing a raw percent escape", () => {
    const routeId = encodeWorkerRouteId("%152");
    expect(routeId).toBe("w25313532");
    expect(routeId).not.toContain("%");
    expect(decodeWorkerRouteId(routeId)).toBe("%152");
  });

  test("round-trip legacy punctuation and unicode display identities", () => {
    for (const workerId of ["mewla-worker-brain:@1", "会话:@7"]) {
      expect(decodeWorkerRouteId(encodeWorkerRouteId(workerId))).toBe(workerId);
    }
  });

  test("a Terminal link without a server opens on the current server", () => {
    expect(resolveTerminalLink("w25313532", undefined, "server-live")).toEqual({ id: "w25313532", serverId: "server-live" });
    expect(resolveTerminalLink("w25313532", "server-live", "server-live")).toEqual({ id: "w25313532", serverId: "server-live" });
  });

  test("a Terminal link for another server or no Worker route resolves to nothing", () => {
    expect(resolveTerminalLink("w25313532", "server-old", "server-live")).toBeNull();
    expect(resolveTerminalLink("w25313532", undefined, null)).toBeNull();
    expect(resolveTerminalLink("", undefined, "server-live")).toBeNull();
    expect(resolveTerminalLink("%152", undefined, "server-live")).toBeNull();
    expect(resolveTerminalLink("zzz", undefined, "server-live")).toBeNull();
  });
});
