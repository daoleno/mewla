import { describe, expect, test } from "bun:test";
import {
  enqueueCurrentServerPersistence,
  resolveCurrentServerId,
  selectCurrentServer,
  shouldPersistResolvedServerId,
} from "./currentServerSelection";

const servers = [
  { id: "server-a", name: "A" },
  { id: "server-b", name: "B" },
];

describe("canonical current-server selection", () => {
  test("a newer explicit switch persists after an older delayed refresh write", async () => {
    let releaseRefreshWrite: () => void = () => undefined;
    const refreshWriteBlocked = new Promise<void>((resolve) => {
      releaseRefreshWrite = resolve;
    });
    let persisted = "server-a";
    let tail = Promise.resolve();

    const refreshWrite = enqueueCurrentServerPersistence(tail, async () => {
      await refreshWriteBlocked;
      persisted = "server-refresh";
    });
    tail = refreshWrite.tail;
    const switchWrite = enqueueCurrentServerPersistence(tail, async () => {
      persisted = "server-b";
    });
    tail = switchWrite.tail;

    releaseRefreshWrite();
    await tail;
    expect(persisted).toBe("server-b");
  });

  test("an explicit switch generation invalidates a refresh before it can persist", async () => {
    let refreshGeneration = 0;
    let persisted = "server-a";
    let tail = Promise.resolve();
    const refreshToken = ++refreshGeneration;

    const switchToken = ++refreshGeneration;
    const switchWrite = enqueueCurrentServerPersistence(tail, async () => {
      persisted = "server-b";
    });
    tail = switchWrite.tail;
    await tail;

    if (refreshToken === refreshGeneration) {
      const staleRefresh = enqueueCurrentServerPersistence(tail, async () => {
        persisted = "server-refresh";
      });
      tail = staleRefresh.tail;
    }
    await tail;

    expect(switchToken).toBe(refreshGeneration);
    expect(persisted).toBe("server-b");
  });

  test("keeps the persisted current server without consulting connection state", () => {
    expect(resolveCurrentServerId(servers, "server-b")).toBe("server-b");
    expect(selectCurrentServer(servers, "server-b")).toEqual(servers[1]);
  });

  test("uses array order only when no identity was ever persisted", () => {
    expect(resolveCurrentServerId(servers, null)).toBe("server-a");
    expect(resolveCurrentServerId(servers, undefined)).toBe("server-a");
    expect(resolveCurrentServerId(servers, "  ")).toBe("server-a");
    expect(resolveCurrentServerId([], null)).toBeNull();
  });

  test("a persisted server that is gone leaves no current server instead of another computer", () => {
    expect(resolveCurrentServerId(servers, "removed")).toBeNull();
    expect(resolveCurrentServerId([], "server-a")).toBeNull();
  });

  test("no current server is never written back, so it cannot turn into a first-computer migration", () => {
    expect(shouldPersistResolvedServerId(null, "removed")).toBe(false);
    expect(shouldPersistResolvedServerId(null, null)).toBe(false);
    expect(shouldPersistResolvedServerId("server-a", "server-a")).toBe(false);
    expect(shouldPersistResolvedServerId("server-a", " server-a ")).toBe(false);
    expect(shouldPersistResolvedServerId("server-a", null)).toBe(true);
    expect(shouldPersistResolvedServerId("server-b", "server-a")).toBe(true);
  });

  test("never invents a current server outside the canonical owner", () => {
    expect(selectCurrentServer(servers, "missing")).toBeNull();
    expect(selectCurrentServer(servers, null)).toBeNull();
  });
});
