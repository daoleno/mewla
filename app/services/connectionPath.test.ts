import { describe, expect, test } from "bun:test";
import { PATH_LABELS, pathMark, type TransportPathInfo } from "./connectionPath";
import { directCandidate } from "./directAddresses";

const lan: TransportPathInfo = { path: "lan", host: "192.168.1.20", rttMs: 12.4 };
const tailscale: TransportPathInfo = { path: "tailscale", host: "100.64.0.9", rttMs: 28 };
const relay: TransportPathInfo = { path: "relay", host: null, rttMs: 140 };

describe("path mark", () => {
  test("LAN names its address, Tailscale and the relay do not", () => {
    expect(pathMark(lan, true)).toEqual({ path: "lan", kind: "direct", label: "LAN", detail: "LAN · 192.168.1.20 · 12 ms" });
    expect(pathMark(tailscale, true)).toEqual({ path: "tailscale", kind: "direct", label: "Tailscale", detail: "Tailscale · 28 ms" });
    expect(pathMark(relay, true)).toEqual({ path: "relay", kind: "relay", label: "Relay", detail: "Relay · 140 ms" });
  });

  test("direct is one kind and the relay another", () => {
    expect([lan, tailscale, relay].map((info) => pathMark(info, true)?.kind)).toEqual(["direct", "direct", "relay"]);
  });

  test("latency is left out when it was not measured", () => {
    expect(pathMark({ ...relay, rttMs: null }, true)?.detail).toBe("Relay");
    expect(pathMark({ ...lan, rttMs: 0 }, true)?.detail).toBe("LAN · 192.168.1.20");
    expect(pathMark({ ...lan, host: null, rttMs: null }, true)?.detail).toBe("LAN");
  });

  test("nothing is said without a path or a live connection", () => {
    expect(pathMark(null, true)).toBeNull();
    expect(pathMark(lan, false)).toBeNull();
    expect(pathMark(relay, false)).toBeNull();
  });

  test("Screen and the indicator share one vocabulary", () => {
    expect(PATH_LABELS.lan).toBe("LAN");
    expect(PATH_LABELS.tailscale).toBe("Tailscale");
    expect(PATH_LABELS.relay).toBe("Relay");
    expect(directCandidate({ host: "192.168.1.20", port: 9876, kind: "lan" }).name).toBe(PATH_LABELS.lan);
    expect(directCandidate({ host: "100.64.0.9", port: 9876, kind: "tailscale" }).name).toBe(PATH_LABELS.tailscale);
  });
});
