// Native has no font stacks: each weight is its own family, and glyphs a face
// lacks (CJK) fall back to the OS face (PingFang SC on iOS, Noto Sans CJK on
// Android). Maple Mono CN carries the common Han set (GB2312 and Big5 level
// 1) at a 2:1 cell; rarer ideographs fall back to the OS face too.
export const FontFamilies = {
  ui: "Inter-Regular",
  uiMedium: "Inter-Medium",
  uiSemibold: "Inter-SemiBold",
  display: "BricolageGrotesque-ExtraBold",
  displaySemibold: "BricolageGrotesque-SemiBold",
  mono: "MapleMono-CN-Regular",
  monoBold: "MapleMono-CN-SemiBold",
} as const;
