import { describe, expect, test } from "bun:test";
import {
  ZEN_BRAND_COLORS,
  ZEN_DARK_CHAT_PALETTE,
  ZEN_LIGHT_CHAT_PALETTE,
  ZEN_DARK_NEUTRALS,
  ZEN_LIGHT_NEUTRALS,
} from "./primitives";

describe("chat outbound send status token", () => {
  test("outboundSentClock is dedicated high-contrast Zen status on chat.background", () => {
    // Light: quiet ink on paper (outside the ink bubble), not bubble fill / outline.
    expect(ZEN_LIGHT_CHAT_PALETTE.outboundSentClock).toBe(ZEN_LIGHT_NEUTRALS.textSecondary);
    expect(ZEN_LIGHT_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_LIGHT_CHAT_PALETTE.sentBubble,
    );
    expect(ZEN_LIGHT_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_LIGHT_CHAT_PALETTE.background,
    );
    expect(ZEN_LIGHT_CHAT_PALETTE.background).toBe(ZEN_LIGHT_NEUTRALS.canvas);

    // Dark: secondary paper on the ink environment — readable outside bubble paint.
    // Same hex as sentTimestamp is intentional: both are high-contrast meta,
    // but outboundSentClock is the status-affordance owner (not bubble chrome).
    expect(ZEN_DARK_CHAT_PALETTE.outboundSentClock).toBe(ZEN_DARK_NEUTRALS.textSecondary);
    expect(ZEN_DARK_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_DARK_CHAT_PALETTE.sentBubble,
    );
    expect(ZEN_DARK_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_DARK_CHAT_PALETTE.background,
    );
    expect(ZEN_DARK_CHAT_PALETTE.background).toBe(ZEN_BRAND_COLORS.environment);
  });
});
