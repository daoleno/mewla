import { expect, test } from "bun:test";

// Only a list that owns Enter counts as a picker: a picker holds Enter on
// desktop (no send, no newline). The edit bar, and Brain's accessory slot
// (which always holds a fragment, even with nothing in it), are not pickers,
// or Enter could never send there.
test("only an open list holds Enter, not the edit bar or an accessory slot", async () => {
  const body = await Bun.file(new URL("./InterfaceChatBody.tsx", import.meta.url)).text();
  expect(body).toContain("accessoryOpen={composerPickerOpen}");
  expect(body).toContain("{composerEditBar}");
  expect(body).not.toMatch(/accessoryOpen=\{[^}]*composerEditBar/);
  expect(body).not.toMatch(/accessoryOpen=\{[^}]*composerAccessory/);
});
