import { describe, expect, test } from "bun:test";
import { callStateTone, connectionStateTone, serviceLead } from "./colour";

describe("an extension's mark (SPEC §10.4)", () => {
  test("a service the client has a real mark for draws it", () => {
    expect(serviceLead("github")).toEqual({ kind: "mark", mark: "github" });
    expect(serviceLead("notion")).toEqual({ kind: "mark", mark: "notion" });
    expect(serviceLead("linear")).toEqual({ kind: "mark", mark: "linear" });
    expect(serviceLead("cloudflare")).toEqual({ kind: "mark", mark: "cloudflare" });
    expect(serviceLead("tailscale")).toEqual({ kind: "mark", mark: "tailscale" });
  });

  test("a one-colour mark of the service's own is drawn in ink", () => {
    expect(serviceLead("aws")).toEqual({ kind: "service", id: "aws" });
    expect(serviceLead("onlora")).toEqual({ kind: "service", id: "onlora" });
    expect(serviceLead("hyperliquid")).toEqual({ kind: "service", id: "hyperliquid" });
  });

  test("a multicolour mark the client lacks is the generic tile, never a recoloured silhouette", () => {
    expect(serviceLead("slack")).toEqual({ kind: "tile", hue: "violet", glyph: "chat" });
    expect(serviceLead("gws")).toEqual({ kind: "tile", hue: "violet", glyph: "apps" });
    expect(serviceLead("gcp")).toEqual({ kind: "tile", hue: "violet", glyph: "cloud" });
  });

  test("a custom service is its kind's glyph, and an unknown one the puzzle, on the violet tile", () => {
    expect(serviceLead("mcp")).toEqual({ kind: "tile", hue: "violet", glyph: "server" });
    expect(serviceLead("openapi")).toEqual({ kind: "tile", hue: "violet", glyph: "code" });
    expect(serviceLead("acme-internal")).toEqual({ kind: "tile", hue: "violet", glyph: "puzzle" });
  });
});

describe("an extension's state (SPEC §10.4)", () => {
  test("connected is ok, off is off, anything that needs a look is warn and never red", () => {
    expect(connectionStateTone("success")).toBe("ok");
    expect(connectionStateTone("neutral")).toBe("off");
    expect(connectionStateTone("warning")).toBe("warn");
    expect(connectionStateTone("danger")).toBe("warn");
  });

  test("a tool call", () => {
    expect(callStateTone("ok")).toBe("ok");
    expect(callStateTone("Succeeded")).toBe("ok");
    expect(callStateTone("permission denied")).toBe("warn");
    expect(callStateTone("error")).toBe("warn");
    expect(callStateTone("cancelled")).toBe("off");
  });
});
