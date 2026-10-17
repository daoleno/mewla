import React from "react";
import Svg, { Path } from "react-native-svg";
import { SERVICE_MARKS } from "./serviceMarkPaths";

export function ServiceMark({ id, size, color }: { id: string; size: number; color: string }) {
  const mark = SERVICE_MARKS[id];
  if (!mark) return null;
  return (
    <Svg width={size} height={size} viewBox={mark.viewBox ?? "0 0 24 24"}>
      <Path d={mark.d} fill={color} fillRule={mark.evenOdd ? "evenodd" : undefined} />
    </Svg>
  );
}
