import { StyleSheet } from "react-native";
import { Radii, TypeScale, UiTextMetrics, type AppColors } from "../../constants/tokens";

export const RESOURCES_CONTENT_MAX_WIDTH = 1360;

export function createResourceStyles(colors: AppColors) {
  return StyleSheet.create({
    section: {
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
      paddingVertical: 12,
      gap: 8,
    },
    sectionHeader: {
      flexDirection: "row",
      alignItems: "baseline",
      justifyContent: "space-between",
      gap: 8,
    },
    sectionTitle: { ...TypeScale.heading, ...UiTextMetrics, color: colors.textPrimary },
    sectionValue: { ...TypeScale.title, ...UiTextMetrics, fontVariant: ["tabular-nums"], color: colors.textPrimary },
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
      flex: 1,
      padding: 12,
      gap: 6,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.sm,
      backgroundColor: colors.bgSurface,
    },
    control: { minHeight: 48, paddingHorizontal: 10, justifyContent: "center", borderRadius: Radii.xs },
    filterRow: { flexDirection: "row", flexWrap: "wrap", gap: 6 },
    filter: { minHeight: 48, paddingHorizontal: 10, paddingVertical: 5, justifyContent: "center", borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.xs },
    tableHeader: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 10, paddingVertical: 8, backgroundColor: colors.surfaceSubtle },
    consumer: { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.borderSubtle },
    consumerButton: { paddingHorizontal: 10, paddingVertical: 10, minHeight: 48, gap: 6 },
    consumerLine: { flexDirection: "row", alignItems: "center", gap: 12 },
    cpuCell: { width: 66, textAlign: "right" },
    rssCell: { width: 76, textAlign: "right" },
    countCell: { width: 45, textAlign: "right" },
    pidCell: { width: 64 },
    processPanel: { padding: 12, gap: 8, backgroundColor: colors.surfaceSubtle, borderRadius: Radii.xs },
    processLine: { flexDirection: "row", alignItems: "flex-start", gap: 12, paddingVertical: 5 },
    chip: {
      paddingHorizontal: 8,
      minHeight: 20,
      borderRadius: 999,
      justifyContent: "center",
    },
  });
}

export type ResourceStyles = ReturnType<typeof createResourceStyles>;
