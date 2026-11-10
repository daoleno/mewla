import { describe, expect, test } from "bun:test";
import {
  blockCreateAfterAmbiguity,
  isCreateBlockedByAmbiguity,
  shouldUnlockCreateAfterAmbiguity,
} from "./createAmbiguityGate";

// From the Expo app's services/providers/providers.test.ts.
describe("Provider stale, reconnect, and ambiguity admission", () => {
  test("ambiguous ordinary create remains blocked across reconnect until a fresh list", () => {
    const blocked = blockCreateAfterAmbiguity(
      {},
      {
        serverId: "server-a",
        connectionGeneration: 3,
        listReceipt: 10,
      },
    );
    expect(
      isCreateBlockedByAmbiguity({
        blocks: blocked,
        serverId: "server-a",
        connectionGeneration: 4,
        listReceipt: 10,
        listFreshForConnection: false,
      }),
    ).toBe(true);
    expect(
      shouldUnlockCreateAfterAmbiguity({
        block: blocked["server-a"],
        connectionGeneration: 4,
        listReceipt: 11,
        listFreshForConnection: true,
      }),
    ).toBe(true);
  });
});
