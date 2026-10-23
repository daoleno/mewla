import { useEffect, useState } from "react";
import { checkComputer, joiningComputers, type ComputerCheck } from "../../services/computerTeam";
import { invalidateStoredServerTransport } from "../../services/pinnedTransport";
import type { StoredServer } from "../../services/storedServerContract";
import { wsClient } from "../../services/websocket";

/**
 * Asks the computers that aren't in use whether they are up while `active`
 * (the switcher is open). The one in use already reports through the
 * connection, and a computer is not offline until it fails to answer, so a
 * row stays blank until its answer arrives.
 */
export function useComputerChecks(
  servers: readonly StoredServer[],
  currentServerId: string | null | undefined,
  active: boolean,
): Record<string, ComputerCheck> {
  const [checks, setChecks] = useState<Record<string, ComputerCheck>>({});
  const key = servers.map((server) => `${server.id}:${server.url}`).join("|");

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    setChecks({});
    const others = servers.filter((server) => server.id !== currentServerId);
    // Close the Link tunnel a check opened, unless the app now uses it or
    // computers are still being introduced over it.
    const release = (server: StoredServer) => {
      if (wsClient.liveServerId() !== server.id && !joiningComputers()) void invalidateStoredServerTransport(server);
    };
    for (const server of others) {
      void checkComputer(server)
        .then((result) => {
          if (!cancelled) setChecks((current) => ({ ...current, [server.id]: result }));
        })
        .finally(() => release(server));
    }
    return () => {
      cancelled = true;
    };
    // `servers` is read through `key`: the effect reruns when a computer or its address changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, currentServerId, key]);

  return checks;
}
