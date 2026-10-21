import { expect, test } from "bun:test";

// The edit bar sits above the composer but is not a picker: a picker holds
// Enter on desktop (no send, no newline), so an edit could never be sent with
// the keyboard when the bar was counted as one.
test("the edit bar does not hold Enter like a picker accessory", async () => {
  const body = await Bun.file(new URL("./InterfaceChatBody.tsx", import.meta.url)).text();
  expect(body).toContain("accessoryOpen={composerAccessory != null}");
  expect(body).toContain("{composerEditBar}");
  expect(body).not.toMatch(/accessoryOpen=\{[^}]*composerEditBar/);
});
