import { describe, expect, test } from "bun:test";
import { reconcileCreateSessionFailure, reconcileCreateSessionSuccess } from "./createSessionReconcile";
import { ProviderError } from "./providerErrors";

// The Expo app had no test for this file; these pin what list.tsx relies on.
describe("create_session outcomes", () => {
  test("a reply without persistence fields is durable and navigable", () => {
    expect(reconcileCreateSessionSuccess({ workerId: "%3" })).toEqual({
      kind: "navigable",
      workerId: "%3",
      persistence: { applied: true, durable: true, outcome: "applied" },
      durabilityWarning: null,
      writeLocked: false,
    });
  });

  test("applied but not durable navigates with a warning; ambiguous blocks further creates", () => {
    const uncertain = reconcileCreateSessionSuccess({ workerId: "%3", persistence: { applied: true, durable: false, outcome: "applied" } });
    expect(uncertain).toMatchObject({ kind: "navigable", writeLocked: true });
    expect(uncertain.kind === "navigable" && uncertain.durabilityWarning).toBeTruthy();
    expect(
      reconcileCreateSessionSuccess({ workerId: "%3", persistence: { applied: false, durable: false, outcome: "unknown", ambiguous: true } }),
    ).toMatchObject({ kind: "ambiguous", requiresReconcileBeforeCreate: true });
    expect(
      reconcileCreateSessionSuccess({ workerId: "%3", persistence: { applied: false, durable: false, outcome: "rejected" } }),
    ).toMatchObject({ kind: "failed", requiresReconcileBeforeCreate: false });
  });

  test("a failure after the request went out is ambiguous unless the daemon said why", () => {
    expect(reconcileCreateSessionFailure(new Error("create_session timed out"), true)).toMatchObject({ kind: "ambiguous" });
    expect(reconcileCreateSessionFailure(new Error("Daemon is not connected."), false)).toMatchObject({ kind: "failed" });
    const incompatible = new ProviderError("route_binding_incompatible", "Not compatible", "incompatible", false);
    expect(reconcileCreateSessionFailure(incompatible, true)).toEqual({
      kind: "failed",
      message: "Not compatible",
      requiresReconcileBeforeCreate: false,
    });
  });
});
