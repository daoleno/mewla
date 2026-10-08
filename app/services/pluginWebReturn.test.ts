import { expect, test } from "bun:test";
import { connectInput, pluginReturnError } from "./pluginOnboarding";

test("the web build sends its origin; native builds don't", () => {
  const input = { integration: "linear", allow_writes: false };
  expect(connectInput(input, "web", "https://mewla.example")).toEqual({ ...input, web_origin: "https://mewla.example" });
  for (const platform of ["ios", "android"]) expect(connectInput(input, platform, "https://mewla.example")).toEqual(input);
  expect(connectInput(input, "web", undefined)).toEqual(input);
});

test("every web return outcome says what to do", () => {
  expect(pluginReturnError(undefined)).toBeNull();
  for (const code of ["expired", "denied", "cancelled", "failed", "unknown"]) expect(pluginReturnError(code)).toMatch(/start again|connect again/i);
});
