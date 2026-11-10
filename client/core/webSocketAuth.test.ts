import { describe, expect, test } from "bun:test";
import { AUTH_SUBPROTOCOL_PREFIX, authSubprotocol } from "./webSocketAuth";

describe("authSubprotocol", () => {
  test("carries the authorization header as a token-safe base64url subprotocol", () => {
    const header = "Mewla v1:device:daemon:1700000000000:abcdef:0123456789abcdef";
    const protocol = authSubprotocol(header);
    expect(protocol.startsWith(AUTH_SUBPROTOCOL_PREFIX)).toBe(true);
    // RFC 6455 subprotocols are HTTP tokens: no spaces, colons, '=', '+' or '/'.
    expect(protocol).toMatch(/^[A-Za-z0-9._-]+$/);
    const decoded = Buffer.from(protocol.slice(AUTH_SUBPROTOCOL_PREFIX.length), "base64url").toString("utf8");
    expect(decoded).toBe(header);
  });
});
