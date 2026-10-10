// Native has no font stacks: each weight is its own family, and glyphs a face
// lacks (CJK) fall back to the OS face (PingFang SC on iOS, Noto Sans CJK on
// Android). The bundled Maple Mono CN is subset to everything but CJK; the
// terminal formatter pins fallback cells to the grid (jni_bridge.cpp).
export const FontFamilies = {
  ui: "Inter-Regular",
  uiMedium: "Inter-Medium",
  uiSemibold: "Inter-SemiBold",
  display: "BricolageGrotesque-ExtraBold",
  displaySemibold: "BricolageGrotesque-SemiBold",
  mono: "MapleMono-CN-Regular",
  monoBold: "MapleMono-CN-SemiBold",
} as const;
