// The bundled CJK faces total ~70 MB; browsers already ship CJK fallbacks,
// so web resolves the same roles to installed families instead.
const UI_STACK =
  '"Source Han Sans SC", "Noto Sans CJK SC", "PingFang SC", "Microsoft YaHei", system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
const MONO_STACK =
  '"Maple Mono CN", "Maple Mono", "JetBrains Mono", "SF Mono", Menlo, Consolas, "Noto Sans Mono CJK SC", ui-monospace, monospace';

export const FontFamilies = {
  ui: UI_STACK,
  uiMedium: UI_STACK,
  mono: MONO_STACK,
  monoBold: MONO_STACK,
} as const;
