import { describe, expect, test } from "bun:test";
import { displayRoute } from "./displayRoute";

describe("displayRoute", () => {
  test("names the worker and nothing else; holding is a message on the socket, not a route", () => {
    expect(displayRoute("w1")).toEqual({ path: "/display/viewer", purpose: "mewla-display-view", params: { worker: "w1" } });
  });
});
