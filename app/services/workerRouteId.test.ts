import { describe, expect, test } from "bun:test";
import { decodeWorkerRouteId, encodeWorkerRouteId, terminalRouteParams } from "./workerRouteId";
import { resolveTerminalRouteWorker } from "../components/terminal/screen/useTerminalRouteModel";

describe("worker route IDs", () => {
  test("round-trip tmux pane IDs without exposing a raw percent escape", () => {
    const routeId = encodeWorkerRouteId("%152");
    expect(routeId).toBe("w25313532");
    expect(routeId).not.toContain("%");
    expect(decodeWorkerRouteId(routeId)).toBe("%152");
  });

  test("round-trip legacy punctuation and unicode display identities", () => {
    for (const workerId of ["zen-worker-brain:@1", "会话:@7"]) {
      expect(decodeWorkerRouteId(encodeWorkerRouteId(workerId))).toBe(workerId);
    }
  });

  test("renders a real daemon pane identity through the Terminal route model", () => {
    const route = terminalRouteParams("%152", "server-live", {
      cwd: "/home/daoleno/workspace/zen",
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
});
