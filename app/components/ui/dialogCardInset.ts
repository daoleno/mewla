type PaddingStyle = {
  padding?: unknown;
  paddingHorizontal?: unknown;
  paddingVertical?: unknown;
  paddingTop?: unknown;
  paddingBottom?: unknown;
};

/** Spacing.xl; kept here so this stays free of react-native for bun tests. */
export const DIALOG_INSET = 24;

/** The phone card's own inset, before a caller's overrides. */
const CARD_INSET = { horizontal: 18, top: 18, bottom: 20 } as const;

/**
 * A desktop dialog's inset. Phone sheets tuck their content close to the
 * grabber and the screen edge; as a dialog, that left the title tight
 * against the rounded corner. Every padded side gets at least DIALOG_INSET,
 * and an explicit 0 stays full-bleed (a preview that draws to the edge).
 */
export function dialogCardInset(overrides: PaddingStyle) {
  const side = (fallback: number, ...values: unknown[]) => {
    const value = values.find((candidate) => candidate !== undefined) ?? fallback;
    if (typeof value !== "number") return undefined;
    return value === 0 ? 0 : Math.max(value, DIALOG_INSET);
  };
  return {
    paddingHorizontal: side(CARD_INSET.horizontal, overrides.paddingHorizontal, overrides.padding),
    paddingTop: side(CARD_INSET.top, overrides.paddingTop, overrides.paddingVertical, overrides.padding),
    paddingBottom: side(CARD_INSET.bottom, overrides.paddingBottom, overrides.paddingVertical, overrides.padding),
  };
}
