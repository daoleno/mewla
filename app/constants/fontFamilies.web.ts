// The bundled CJK faces total ~70 MB; browsers already ship CJK fallbacks,
// so web leads with the small Latin faces (Inter, Bricolage Grotesque; see
// appFontAssets.web.ts) and resolves CJK through installed families. Mono
// leads with a Latin/symbol subset of Maple Mono: xterm sizes cells from the
// first family, and generic `monospace` can resolve to a proportional CJK
// face on Linux.
export const WEB_MONO_FAMILY = "Mewla Maple Mono";
export const WEB_MONO_BOLD_FAMILY = "Mewla Maple Mono SemiBold";
export const WEB_UI_FAMILY = "Inter";
export const WEB_UI_MEDIUM_FAMILY = "Inter Medium";
export const WEB_UI_SEMIBOLD_FAMILY = "Inter SemiBold";
export const WEB_DISPLAY_FAMILY = "Bricolage Grotesque ExtraBold";
export const WEB_DISPLAY_SEMIBOLD_FAMILY = "Bricolage Grotesque SemiBold";

const CJK_STACK =
  '"Source Han Sans SC", "Noto Sans CJK SC", "PingFang SC", "Microsoft YaHei", system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
const MONO_FALLBACK =
  '"Maple Mono CN", "Noto Sans Mono CJK SC", "Source Han Sans SC", "PingFang SC", "Microsoft YaHei", monospace';

export const FontFamilies = {
  ui: `"${WEB_UI_FAMILY}", ${CJK_STACK}`,
  uiMedium: `"${WEB_UI_MEDIUM_FAMILY}", "${WEB_UI_FAMILY}", ${CJK_STACK}`,
  uiSemibold: `"${WEB_UI_SEMIBOLD_FAMILY}", "${WEB_UI_FAMILY}", ${CJK_STACK}`,
  display: `"${WEB_DISPLAY_FAMILY}", "${WEB_UI_SEMIBOLD_FAMILY}", ${CJK_STACK}`,
  displaySemibold: `"${WEB_DISPLAY_SEMIBOLD_FAMILY}", "${WEB_UI_SEMIBOLD_FAMILY}", ${CJK_STACK}`,
  cjk: CJK_STACK,
  cjkMedium: CJK_STACK,
  mono: `"${WEB_MONO_FAMILY}", ${MONO_FALLBACK}`,
  monoBold: `"${WEB_MONO_BOLD_FAMILY}", "${WEB_MONO_FAMILY}", ${MONO_FALLBACK}`,
} as const;
