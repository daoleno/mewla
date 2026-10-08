import { describe, expect, test } from "bun:test";
import { decodeWorkerRouteId, encodeWorkerRouteId, resolveTerminalLink, terminalRouteParams } from "./workerRouteId";
import { resolveTerminalRouteWorker } from "../components/terminal/screen/useTerminalRouteModel";

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

  test("renders a real daemon pane identity through the Terminal route model", () => {
    const route = terminalRouteParams("%152", "server-live", {
      cwd: "/home/daoleno/workspace/mewla",
      command: "zsh",
      name: "Shell",
    });
    const workerId = decodeWorkerRouteId(route.id);
    const sessionKey = JSON.stringify([route.serverId, workerId]);
    const worker = resolveTerminalRouteWorker({
      routeSessionHint: {
        cwd: route.cwd,
        command: route.command,
        name: route.name,
      },
      sessionKey,
      serverId: route.serverId,
      workerId,
    });

    expect(route.id).toBe("w25313532");
    expect(worker).toMatchObject({ id: "%152", serverId: "server-live", command: "zsh" });
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
