// The bundled CJK faces total ~70 MB; browsers already ship CJK fallbacks,
// so web resolves the same roles to installed families instead. Mono leads
// with a Latin/symbol subset of Maple Mono (appFontAssets.web.ts): xterm sizes
// cells from the first family, and generic `monospace` can resolve to a
// proportional CJK face on Linux.
export const WEB_MONO_FAMILY = "Zen Maple Mono";
export const WEB_MONO_BOLD_FAMILY = "Zen Maple Mono SemiBold";

const UI_STACK =
  '"Source Han Sans SC", "Noto Sans CJK SC", "PingFang SC", "Microsoft YaHei", system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
const MONO_FALLBACK =
  '"Maple Mono CN", "Noto Sans Mono CJK SC", "Source Han Sans SC", "PingFang SC", "Microsoft YaHei", monospace';

export const FontFamilies = {
  ui: UI_STACK,
  uiMedium: UI_STACK,
  mono: `"${WEB_MONO_FAMILY}", ${MONO_FALLBACK}`,
  monoBold: `"${WEB_MONO_BOLD_FAMILY}", "${WEB_MONO_FAMILY}", ${MONO_FALLBACK}`,
} as const;
