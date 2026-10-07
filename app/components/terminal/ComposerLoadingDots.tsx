import React from "react";
import { ActivityIndicator, View } from "react-native";

interface ComposerLoadingDotsProps {
  color: string;
  size?: number;
  gap?: number;
}
export function ComposerLoadingDots({
  color,
  size = 10,
}: ComposerLoadingDotsProps) {
  const footprint = Math.max(size * 2.2, size + 8);
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      pointerEvents="none"
      style={{
        width: footprint,
        height: footprint,
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <ActivityIndicator size="small" color={color} />
    </View>
  );
}
