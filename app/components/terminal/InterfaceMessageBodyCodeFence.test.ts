import { describe, expect, test } from "bun:test";
import { parseMessageBlocks } from "./InterfaceMessageBodyModel";

describe("Message body code fences", () => {
  test("a fence inside a list item drops the item's indent from its code", () => {
    const blocks = parseMessageBlocks("1. Run it:\n\n   ```bash\n   cd app\n     bun test\n   ```");
    expect(blocks.find((block) => block.type === "code")).toMatchObject({
      type: "code",
      text: "cd app\n  bun test",
      language: "bash",
    });
  });

  test("code indented less than its fence keeps what it has", () => {
    const blocks = parseMessageBlocks("   ```\n x\n```");
    expect(blocks.find((block) => block.type === "code")).toMatchObject({ text: "x" });
  });
});
