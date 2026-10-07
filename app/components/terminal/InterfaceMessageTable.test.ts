// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { buildChatChrome } from "../../theme/buildChatChrome";
import { resolveTheme } from "../../theme/resolve";
import {
  messageTableRowTone,
  messageTableSemanticColors,
} from "./InterfaceMessageTableModel";

function rgbDistance(left: string, right: string) {
  const channels = (value: string) =>
    [1, 3, 5].map((index) =>
      Number.parseInt(value.slice(index, index + 2), 16),
    );
  const leftChannels = channels(left);
  const rightChannels = channels(right);
  return Math.sqrt(
    leftChannels.reduce(
      (sum, channel, index) => sum + (channel - rightChannels[index]) ** 2,
      0,
    ),
  );
}

describe("Interface Markdown table semantics", () => {
  test.each(["light", "dark"] as const)(
    "%s theme keeps the true header distinct from quieter section rows",
    (colorScheme) => {
      const { chrome } = buildChatChrome(resolveTheme({ colorScheme }));
      const colors = messageTableSemanticColors(chrome);

      expect(colors.header).not.toBe(colors.section);
      expect(colors.header).not.toBe(colors.odd);
      expect(rgbDistance(colors.header, colors.section)).toBeGreaterThan(30);
      expect(rgbDistance(colors.section, colors.even)).toBeLessThan(
        rgbDistance(colors.header, colors.even),
      );
    },
  );

  test("a single populated cell is a semantic section row", () => {
    expect(messageTableRowTone(["**Platform**", "", ""], 1)).toBe("section");
    expect(messageTableRowTone(["iOS", "supported", "native"], 1)).toBe("odd");
  });
});
