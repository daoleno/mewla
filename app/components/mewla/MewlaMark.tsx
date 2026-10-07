import React from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import Svg, { Circle, G, Path } from "react-native-svg";
import { CurledCat } from "./SealCat";
import { CAT_IN_SEAL, SEAL, SEAL_PAPER, SEAL_RED, sealCarving } from "./sealCatGeometry";

const BLOCK = [SEAL.block, ...SEAL.chips].join(" ");

/**
 * The Mewla mark: seal-icon.svg's vermilion seal with the curled paper cat
 * under a crescent moon, as on the landing's header. It is the logo, not
 * Brain, so it never moves or changes with Brain's state. The edge chips are
 * cut through, so whatever surface holds the mark shows in them.
 */
export function MewlaMark({ size, style }: { size: number; style?: StyleProp<ViewStyle> }) {
  const carving = sealCarving(size);
  return (
    <View
      accessible={false}
      importantForAccessibility="no-hide-descendants"
      style={[{ width: size, height: size }, style]}
    >
      <Svg viewBox="0 0 100 100" width={size} height={size}>
        <Path d={BLOCK} fill={SEAL_RED} fillRule="evenodd" />
        <Circle {...SEAL.moon} fill={SEAL_PAPER} />
        <Circle {...SEAL.moonBite} fill={SEAL_RED} />
        <G transform={CAT_IN_SEAL}>
          <CurledCat fill={SEAL_PAPER} line={SEAL_RED} {...carving} />
        </G>
      </Svg>
    </View>
  );
}
