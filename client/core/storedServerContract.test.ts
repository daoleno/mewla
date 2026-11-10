import { describe, expect, test } from "bun:test";
import {
  mergeStoredServer,
  normalizeStoredServers,
  serverNamedByAddress,
} from "./storedServerContract";

describe("one StoredServer transport candidate migration", () => {
  test("loads a legacy Pairing V1 record without migration loss", () => {
    const servers = normalizeStoredServers([
      {
        id: "legacy",
        name: "Cloudflare workstation",
        url: "wss://mewla.example/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
      },
    ]);
    expect(servers).toHaveLength(1);
    expect(servers[0]).toMatchObject({
      id: "legacy",
      name: "Cloudflare workstation",
      url: "wss://mewla.example/ws",
      transportKind: "manual",
      transportCandidates: [
        {
          name: "Manual",
          kind: "manual",
          url: "wss://mewla.example/ws",
        },
      ],
    });
  });

  test("re-importing Link updates the same daemon record, not a region server", () => {
    let nextId = 0;
    const createId = () => `server-${++nextId}`;
    const first = mergeStoredServer(
      {
        name: "Workstation",
        url: "wss://route.a.link.test/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "link",
        transportPin: "3".repeat(64),
        linkRouteId: "4".repeat(32),
        transportCandidates: [
          { name: "a", kind: "link", url: "wss://route.a.link.test/ws" },
        ],
      },
      [],
      createId,
    );
    const second = mergeStoredServer(
      {
        name: "",
        url: "wss://route.b.link.test/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "link",
        transportPin: "3".repeat(64),
        linkRouteId: "4".repeat(32),
        transportCandidates: [
          { name: "b", kind: "link", url: "wss://route.b.link.test/ws" },
        ],
      },
      first.servers,
      createId,
    );
    expect(second.server.id).toBe(first.server.id);
    expect(second.server.name).toBe("Workstation");
    expect(second.servers).toHaveLength(1);
  });
});

describe("naming a computer", () => {
  const link = {
    id: "mac",
    name: "swift-hill-a9de.m.mewla.ai",
    url: "wss://swift-hill-a9de.m.mewla.ai/ws",
    daemonId: "1".repeat(64),
    daemonPublicKey: "2".repeat(64),
    transportKind: "link" as const,
    transportPin: "3".repeat(64),
    linkRouteId: "4".repeat(32),
    transportCandidates: [{ name: "a", kind: "link" as const, url: "wss://swift-hill-a9de.m.mewla.ai/ws" }],
  };

  test("a computer still known by its address takes the name it reports", () => {
    expect(serverNamedByAddress(link)).toBe(true);
    expect(serverNamedByAddress({ ...link, name: "Studio" })).toBe(false);
  });

  test("renaming keeps the Mewla Link identity", () => {
    const { server } = mergeStoredServer({ ...link, name: "daoleno's MacBook Pro" }, [link], () => "new");
    expect(server).toMatchObject({
      id: "mac",
      name: "daoleno's MacBook Pro",
      transportKind: "link",
      transportPin: link.transportPin,
      linkRouteId: link.linkRouteId,
      transportCandidates: link.transportCandidates,
    });
  });
});
