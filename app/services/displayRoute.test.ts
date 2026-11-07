import { describe, expect, test } from "bun:test";
import { displayRoute } from "./displayRoute";

describe("displayRoute", () => {
  test("watching names the worker and nothing else", () => {
    expect(displayRoute("w1", false)).toEqual({ path: "/display/viewer", purpose: "mewla-display-view", params: { worker: "w1" } });
  });

  test("taking over says so in the request", () => {
    expect(displayRoute("w1", true).params).toEqual({ worker: "w1", takeover: "1" });
  });
});
