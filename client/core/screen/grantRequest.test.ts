import { expect, test } from "bun:test";
import { grantRequestAction } from "./grantRequest";
import { installMemoryPlatform } from "../testing/memoryPlatform";

test("a card's button is a signed POST of the card's id and the action", async () => {
  const calls: Array<{ url: string; method?: string; body?: unknown; authorization?: string }> = [];
  installMemoryPlatform({
    fetch: async (input, init) => {
      calls.push({ url: String(input), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined, authorization: (init?.headers as Record<string, string> | undefined)?.Authorization });
      return new Response(JSON.stringify({ status: "opened" }), { status: 200, headers: { "Content-Type": "application/json" } });
    },
  });
  const server = { id: "s", name: "This Mac", url: "http://127.0.0.1:9876", daemonId: "d".repeat(64), daemonPublicKey: "e".repeat(64) } as never;
  await expect(grantRequestAction(server, "grant:screen_recording:1", "open_settings")).resolves.toEqual({ status: "opened" });
  expect(calls[0]?.url.endsWith("/brain/grant-request")).toBe(true);
  expect(calls[0]?.method).toBe("POST");
  expect(calls[0]?.body).toEqual({ id: "grant:screen_recording:1", action: "open_settings" });
});
