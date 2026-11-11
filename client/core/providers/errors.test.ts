import { describe, expect, test } from "bun:test";
import { PROVIDER_ERROR_CODES, providerErrorFromPayload } from "./errors";

describe("a Provider error's words", () => {
  // FINAL-VERIFY 6387f88f m4 settings [182]: an unsaved Test connection to an endpoint that
  // refused said "Providers are not available on this daemon." instead of why it failed.
  test("an unsaved connection test that failed says why, without the key", () => {
    const error = providerErrorFromPayload(
      { code: PROVIDER_ERROR_CODES.testFailed, message: 'Get "http://127.0.0.1:9/sk-live-123/models": dial tcp 127.0.0.1:9: connect: connection refused' },
      { credentialWrite: true, secret: "sk-live-123" },
    );
    expect(error.kind).toBe("test_failed");
    expect(error.message).toContain("connection refused");
    expect(error.message).not.toContain("sk-live-123");
  });

  test("another refusal of a request that carried a key still says only the kind's words", () => {
    const error = providerErrorFromPayload({ code: "credential_store_failed", message: "store said sk-live-123" }, { credentialWrite: true, secret: "sk-live-123" });
    expect(error.message).toBe("The Provider API key could not be updated. You can retry from Providers.");
  });
});
