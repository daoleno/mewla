import { StyleSheet } from "react-native";
import { Radii, TypeScale, UiTextMetrics, type AppColors } from "../../constants/tokens";

export const RESOURCES_CONTENT_MAX_WIDTH = 760;

export function createResourceStyles(colors: AppColors) {
  return StyleSheet.create({
    section: {
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
      paddingVertical: 18,
      gap: 12,
    },
    sectionHeader: {
      flexDirection: "row",
      alignItems: "baseline",
      justifyContent: "space-between",
      gap: 12,
    },
    sectionTitle: { ...TypeScale.heading, ...UiTextMetrics, color: colors.textPrimary },
    sectionValue: { ...TypeScale.title, ...UiTextMetrics, color: colors.textPrimary },
    sectionNote: { ...TypeScale.compact, ...UiTextMetrics, color: colors.textSecondary },
    caption: { ...TypeScale.caption, ...UiTextMetrics, color: colors.textTertiary },
    micro: { ...TypeScale.micro, ...UiTextMetrics, color: colors.textTertiary },
    mono: { ...TypeScale.mono, ...UiTextMetrics, color: colors.textSecondary },
    monoStrong: { ...TypeScale.monoStrong, ...UiTextMetrics, color: colors.textPrimary },
    label: { ...TypeScale.label, ...UiTextMetrics, color: colors.textPrimary },
    axisRow: { flexDirection: "row", justifyContent: "space-between", marginTop: 6 },
    legendRow: { flexDirection: "row", flexWrap: "wrap", columnGap: 16, rowGap: 6 },
    legendItem: { flexDirection: "row", alignItems: "center", gap: 6 },
    legendSwatch: { width: 8, height: 8, borderRadius: 4 },
    factsRow: { flexDirection: "row", gap: 12 },
    fact: { flex: 1, minWidth: 0, gap: 2 },
    surface: {
      padding: 16,
      gap: 10,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.md,
      backgroundColor: colors.bgSurface,
    },
    chip: {
      paddingHorizontal: 8,
      minHeight: 20,
      borderRadius: 999,
      justifyContent: "center",
    },
  });
}

export type ResourceStyles = ReturnType<typeof createResourceStyles>;
