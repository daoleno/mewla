import { describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { diagnoseConnectionIssue, issueCardDetail, type DiagnoseDeps } from "./connectionIssue";

const relayComputer: StoredServer = {
  id: "s1",
  name: "DESKTOP-I57K0FC",
  url: "https://amber-lake-3845.m.mewla.ai",
  daemonId: "a".repeat(64),
  daemonPublicKey: "b".repeat(64),
  transportKind: "link",
  transportPin: "c".repeat(64),
  linkRouteId: "d".repeat(32),
};
const addressComputer: StoredServer = { ...relayComputer, url: "https://mewla.example.com", transportKind: "manual", transportPin: undefined, linkRouteId: undefined };

/** A probe whose every request fails as the phone's fetch failed. */
function failing(error: Error): DiagnoseDeps {
  return { fetch: () => Promise.reject(error), authorization: async () => "Mewla test" };
}

async function diagnose(server: StoredServer, message: string, name = "TypeError") {
  return diagnoseConnectionIssue({ server, socketError: "socket closed" }, failing(Object.assign(new Error(message), { name })));
}

describe("a computer paired through Mewla Relay", () => {
  // The relay closes at once a connection to a computer that is not connected to it. Each platform words it
  // as TLS/SSL; it was shown as "TLS handshake failed … check your certificate chain … reverse proxy" (11-16).
  for (const message of [
    "An SSL error has occurred and a secure connection to the server cannot be made.", // iOS URLSession -1200
    "Network request failed: io: unexpected end of file", // NSH's fetch (rustls), measured against the live relay
    "发生了SSL错误，无法建立与该服务器的安全连接。", // iOS -1200 in Chinese
    "Connection reset by peer",
  ]) {
    test(`is named not connected to the relay, never a certificate problem: ${message}`, async () => {
      const found = await diagnose(relayComputer, message);
      expect(found.code).toBe("relay_not_connected");
      expect(found.title).toBe("DESKTOP-I57K0FC isn't online");
      expect(found.hint).toContain("mewla doctor");
      // A network that resets TLS to the relay looks the same from the phone: the card names it too.
      expect(found.hint).toContain("another network");
      expect(found.raw).toBe(message);
      expect(`${found.title} ${found.hint}`).not.toMatch(/certificate|reverse proxy|TLS handshake/i);
    });
  }

  test("a computer without a name is This computer", async () => {
    const found = await diagnose({ ...relayComputer, name: "" }, "Network request failed: io: unexpected end of file");
    expect(found.title).toBe("This computer isn't online");
  });

  test("a certificate the phone does not trust means the computer answered: not off the relay", async () => {
    const found = await diagnose(relayComputer, "Network request failed: io: invalid peer certificate: UnknownIssuer");
    expect(found.code).toBe("network_unreachable");
    expect(found.title).toBe("Connection through Mewla Relay failed");
    expect(found.hint).toContain("mewla doctor");
  });

  test("no answer in time, or no network, is the phone not reaching the relay", async () => {
    const timedOut = await diagnose(relayComputer, "timed out", "AbortError");
    expect(timedOut.title).toBe("Can't reach Mewla Relay");
    const offline = await diagnose(relayComputer, "The Internet connection appears to be offline.");
    expect(offline.title).toBe("Can't reach Mewla Relay");
    expect(offline.raw).toBe("The Internet connection appears to be offline.");
  });

  test("the card says what to do, then the error word for word", async () => {
    const found = await diagnose(relayComputer, "Network request failed: io: unexpected end of file");
    expect(issueCardDetail(found)).toBe(`${found.hint}\nNetwork request failed: io: unexpected end of file`);
  });
});

describe("a computer paired by address", () => {
  test("a TLS failure is still the address's TLS, with the raw error on the card", async () => {
    const found = await diagnose(addressComputer, "An SSL error has occurred and a secure connection to the server cannot be made.");
    expect(found.title).toBe("TLS handshake failed");
    expect(found.raw).toBe("An SSL error has occurred and a secure connection to the server cannot be made.");
    // The raw error is already the detail here; the card shows the hint and the error once.
    expect(issueCardDetail(found)).toBe(`${found.hint}\n${found.raw}`);
  });

  test("an issue with nothing raw shows its hint alone", () => {
    expect(issueCardDetail({ code: "path_not_found", title: "t", detail: "d", hint: "h" })).toBe("h");
  });
});
