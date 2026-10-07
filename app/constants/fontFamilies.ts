// Native has no font stacks: each weight is its own family, and glyphs a face
// lacks (CJK) fall back to the OS face (PingFang SC on iOS, Noto Sans CJK on
// Android). Source Han Sans SC stays bundled as the explicit `cjk` role.
export const FontFamilies = {
  ui: "Inter-Regular",
  uiMedium: "Inter-Medium",
  uiSemibold: "Inter-SemiBold",
  display: "BricolageGrotesque-ExtraBold",
  displaySemibold: "BricolageGrotesque-SemiBold",
  cjk: "SourceHanSansSC-Regular",
  cjkMedium: "SourceHanSansSC-Medium",
  mono: "MapleMono-CN-Regular",
  monoBold: "MapleMono-CN-SemiBold",
} as const;
