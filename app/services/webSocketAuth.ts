/**
 * The browser WebSocket API cannot set an Authorization header, and a
 * credential in the URL lands in proxy and tunnel logs. A browser instead
 * offers one subprotocol, `mewla.auth.<base64url of the header value>`, which
 * the daemon reads in place of the header and echoes back. The signature,
 * purpose and single-use nonce checks are the header's, unchanged.
 */
export const AUTH_SUBPROTOCOL_PREFIX = "mewla.auth.";

export function authSubprotocol(authorization: string): string {
  const bytes = new TextEncoder().encode(authorization);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  const encoded = globalThis
    .btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
  return AUTH_SUBPROTOCOL_PREFIX + encoded;
}
